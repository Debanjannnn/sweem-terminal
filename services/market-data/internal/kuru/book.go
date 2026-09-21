package kuru

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/book"
)

// Two OrderBook generations are live on Monad. The deployed testnet
// implementation answers getL2Book(uint256) with four uint256[]; the
// SDK-documented generation answers getL2Book() with a packed bytes blob. Try
// the first, fall back to the second -- this fallback is why the old stack's
// reader kept working through a deployment it did not know about.
const (
	selArrays = "b4cc4b1a" // getL2Book(uint256)
	selPacked = "46fdfbb1" // getL2Book()

	DefaultDepth = 20
)

type pair struct{ price, size *big.Int }

// Book reads the L2 book on chain and scales it into WAD.
func (c *Client) Book(ctx context.Context, marketID string, depth int) (book.Book, error) {
	m, ok := c.Market(marketID)
	if !ok {
		return book.Book{}, fmt.Errorf("market %q is not in the Kuru market list", marketID)
	}
	if depth <= 0 {
		depth = DefaultDepth
	}
	bids, asks, err := c.rawBook(ctx, m.Address, depth)
	if err != nil {
		return book.Book{}, err
	}
	b := book.Book{
		MarketID: m.ID,
		Venue:    m.Venue,
		Bids:     levels(bids, m.PriceDecimals, m.SizeDecimals, depth),
		Asks:     levels(asks, m.PriceDecimals, m.SizeDecimals, depth),
		Ts:       time.Now().UnixMilli(),
	}
	book.Sort(b.Bids, true)
	book.Sort(b.Asks, false)
	return b, nil
}

func (c *Client) rawBook(ctx context.Context, addr string, depth int) ([]pair, []pair, error) {
	call, _ := hex.DecodeString(selArrays)
	if out, err := c.ethCall(ctx, addr, append(call, word(big.NewInt(int64(depth)))...)); err == nil {
		if bids, asks, derr := decodeArrayBook(out); derr == nil {
			return bids, asks, nil
		}
	}
	call, _ = hex.DecodeString(selPacked)
	out, err := c.ethCall(ctx, addr, call)
	if err != nil {
		return nil, nil, fmt.Errorf("neither getL2Book ABI answered on %s: %w", addr, err)
	}
	return decodePackedBook(out)
}

// decodeArrayBook reads (uint256[] bidPrices, bidSizes, askPrices, askSizes).
func decodeArrayBook(data []byte) ([]pair, []pair, error) {
	arrays := make([][]*big.Int, 4)
	for i := range 4 {
		off, err := wordAt(data, i*32)
		if err != nil {
			return nil, nil, err
		}
		arr, err := readArray(data, off)
		if err != nil {
			return nil, nil, err
		}
		arrays[i] = arr
	}
	return zip(arrays[0], arrays[1]), zip(arrays[2], arrays[3]), nil
}

// decodePackedBook reads the older generation's `bytes` blob:
// [blockNumber, (price,size)..., 0, (price,size)...]. Ported as-is: the zero
// terminator consumes a whole pair slot, which is what the live older
// deployment emitted.
// ponytail: ported terminator handling, revisit if a packed book ever decodes short
func decodePackedBook(data []byte) ([]pair, []pair, error) {
	off, err := wordAt(data, 0)
	if err != nil {
		return nil, nil, err
	}
	n, err := wordAt(data, int(off))
	if err != nil {
		return nil, nil, err
	}
	start := int(off) + 32
	if start+int(n) > len(data) {
		return nil, nil, fmt.Errorf("packed book claims %d bytes but only %d are present", n, len(data)-start)
	}
	blob := data[start : start+int(n)]
	if len(blob) < 32 {
		return nil, nil, fmt.Errorf("packed book is %d bytes, too short to hold a block number", len(blob))
	}
	words := make([]*big.Int, 0, len(blob)/32)
	for i := 32; i+32 <= len(blob); i += 32 { // word 0 is the block number
		words = append(words, new(big.Int).SetBytes(blob[i:i+32]))
	}
	var sides [2][]pair
	i := 0
	for s := range 2 {
		for i+1 < len(words) {
			price := words[i]
			i += 2
			if price.Sign() == 0 {
				break
			}
			sides[s] = append(sides[s], pair{price: price, size: words[i-1]})
		}
	}
	return sides[0], sides[1], nil
}

func readArray(data []byte, off uint64) ([]*big.Int, error) {
	n, err := wordAt(data, int(off))
	if err != nil {
		return nil, err
	}
	out := make([]*big.Int, 0, n)
	for i := range int(n) {
		// elements are prices and sizes, not offsets: no upper bound applies
		b, err := rawWordAt(data, int(off)+32+i*32)
		if err != nil {
			return nil, err
		}
		out = append(out, new(big.Int).SetBytes(b))
	}
	return out, nil
}

// wordAt returns the 32-byte word at offset as a uint64, rejecting anything
// that cannot be an offset or a length.
func wordAt(data []byte, offset int) (uint64, error) {
	b, err := rawWordAt(data, offset)
	if err != nil {
		return 0, err
	}
	n := new(big.Int).SetBytes(b)
	if !n.IsUint64() || n.Uint64() > uint64(len(data)) {
		return 0, fmt.Errorf("word at %d is %s, which cannot index %d bytes of return data", offset, n, len(data))
	}
	return n.Uint64(), nil
}

func rawWordAt(data []byte, offset int) ([]byte, error) {
	if offset < 0 || offset+32 > len(data) {
		return nil, fmt.Errorf("return data is %d bytes, too short to hold a word at %d", len(data), offset)
	}
	return data[offset : offset+32], nil
}

func zip(prices, sizes []*big.Int) []pair {
	out := make([]pair, 0, len(prices))
	for i, p := range prices {
		s := new(big.Int)
		if i < len(sizes) {
			s = sizes[i]
		}
		out = append(out, pair{price: p, size: s})
	}
	return out
}

func levels(ps []pair, priceDecimals, sizeDecimals, depth int) []book.Level {
	out := make([]book.Level, 0, len(ps))
	for _, p := range ps {
		if p.price.Sign() <= 0 || p.size.Sign() <= 0 {
			continue
		}
		if len(out) >= depth {
			break
		}
		out = append(out, book.Level{
			Price: wad.Scale(p.price, priceDecimals, wad.Decimals).String(),
			Size:  wad.Scale(p.size, sizeDecimals, wad.Decimals).String(),
		})
	}
	return out
}
