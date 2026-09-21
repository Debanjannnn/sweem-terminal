package kuru

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
)

// fakeRPC answers eth_call per selector. A selector mapped to "" reverts, which
// is how the dual-ABI fallback gets exercised.
type fakeRPC struct {
	bySelector map[string]string
	lastData   string
	calls      []string
}

func (f *fakeRPC) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		json.Unmarshal(body, &req)
		data := ""
		if len(req.Params) > 0 {
			if m, ok := req.Params[0].(map[string]any); ok {
				data, _ = m["data"].(string)
			}
		}
		f.lastData = data
		sel := strings.TrimPrefix(data, "0x")
		if len(sel) >= 8 {
			sel = sel[:8]
		}
		f.calls = append(f.calls, sel)
		out, ok := f.bySelector[sel]
		if !ok || out == "" {
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"execution reverted"}}`))
			return
		}
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x` + out + `"}`))
	}
}

func hexWord(n int64) string { return hex.EncodeToString(word(big.NewInt(n))) }

// encodeArrays builds the (uint256[],uint256[],uint256[],uint256[]) return.
func encodeArrays(arrays ...[]int64) string {
	head, tail := "", ""
	offset := int64(len(arrays) * 32)
	for _, a := range arrays {
		head += hexWord(offset)
		tail += hexWord(int64(len(a)))
		for _, v := range a {
			tail += hexWord(v)
		}
		offset += int64(32 + len(a)*32)
	}
	return head + tail
}

// encodePacked builds the older generation's `bytes` return.
func encodePacked(words ...int64) string {
	blob := ""
	for _, w := range words {
		blob += hexWord(w)
	}
	return hexWord(32) + hexWord(int64(len(blob)/2)) + blob
}

func bookClient(t *testing.T, f *fakeRPC) *Client {
	t.Helper()
	rpc := httptest.NewServer(f.handler())
	t.Cleanup(rpc.Close)
	c := serve(t, chains.MonadTestnet, testnetPayload)
	if _, err := c.Markets(context.Background()); err != nil {
		t.Fatalf("seed markets: %v", err)
	}
	c.rpcURL = rpc.URL
	return c
}

const market = "0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f"

func TestBookArraysABI(t *testing.T) {
	// CBBTCUSDC: priceDecimals 2, sizeDecimals 8
	f := &fakeRPC{bySelector: map[string]string{
		selArrays: encodeArrays(
			[]int64{11_000_000, 10_900_000},  // bid prices
			[]int64{50_000_000, 100_000_000}, // bid sizes
			[]int64{11_100_000},              // ask prices
			[]int64{25_000_000},              // ask sizes
		),
	}}
	b, err := bookClient(t, f).Book(context.Background(), strings.ToUpper(market), 20)
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	if b.MarketID != market || b.Venue != "kuru" {
		t.Fatalf("book identity = %s/%s", b.MarketID, b.Venue)
	}
	if got, want := b.Bids[0].Price, "110000000000000000000000"; got != want {
		t.Errorf("bid price = %s, want %s", got, want)
	}
	if got, want := b.Bids[0].Size, "500000000000000000"; got != want {
		t.Errorf("bid size = %s, want %s", got, want)
	}
	if got, want := b.Asks[0].Price, "111000000000000000000000"; got != want {
		t.Errorf("ask price = %s, want %s", got, want)
	}
	if len(b.Bids) != 2 || len(b.Asks) != 1 {
		t.Fatalf("depth = %d bids %d asks", len(b.Bids), len(b.Asks))
	}
	if want := "0x" + selArrays + hexWord(20); f.lastData != want {
		t.Errorf("call data = %s, want depth 20 encoded as %s", f.lastData, want)
	}
}

func TestBookFallsBackToPackedABI(t *testing.T) {
	f := &fakeRPC{bySelector: map[string]string{
		selArrays: "", // the deployed generation this codebase does not have
		// block, bidPrice, bidSize, terminator pair, askPrice, askSize
		selPacked: encodePacked(999, 11_000_000, 50_000_000, 0, 0, 11_100_000, 25_000_000),
	}}
	c := bookClient(t, f)
	b, err := c.Book(context.Background(), market, 20)
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	if len(f.calls) != 2 || f.calls[0] != selArrays || f.calls[1] != selPacked {
		t.Fatalf("calls = %v, want the arrays ABI tried first", f.calls)
	}
	if len(b.Bids) != 1 || b.Bids[0].Price != "110000000000000000000000" {
		t.Fatalf("bids = %v", b.Bids)
	}
	if len(b.Asks) != 1 || b.Asks[0].Price != "111000000000000000000000" {
		t.Fatalf("asks = %v", b.Asks)
	}
}

func TestBookRejectsJunk(t *testing.T) {
	cases := map[string]string{
		"truncated head":      hexWord(128),
		"offset past the end": hexWord(1 << 20),
		"empty return":        "",
	}
	for name, packed := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeRPC{bySelector: map[string]string{selArrays: "", selPacked: packed}}
			if _, err := bookClient(t, f).Book(context.Background(), market, 20); err == nil {
				t.Fatal("want an error, got a book")
			}
		})
	}
}

func TestBookUnknownMarket(t *testing.T) {
	f := &fakeRPC{bySelector: map[string]string{}}
	if _, err := bookClient(t, f).Book(context.Background(), "0xdeadbeef", 20); err == nil {
		t.Fatal("want an error for a market that is not in the list")
	}
}
