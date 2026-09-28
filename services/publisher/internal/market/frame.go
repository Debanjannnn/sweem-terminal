package market

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// Frame kinds, kuru-spot-v2.md §6.2.
const (
	kindBook   = 1
	kindDelta  = 2
	kindTrades = 3
)

type level struct {
	Px int64
	Sz *big.Int
}

type update struct {
	Buy bool
	level
}

type fill struct {
	Buy bool // taker side
	Px  int64
	Sz  *big.Int
	Ts  int64 // block time, unix s
}

type frame struct {
	ID     uint64
	Kind   byte
	Epoch  uint64
	Market string
	Seq    uint64
	Prev   uint64 // 0 = null
	Bids   []level
	Asks   []level
	Update []update
	Fills  []fill
}

var errShort = errors.New("short frame")

type rd struct {
	b   []byte
	err error
}

func (r *rd) take(n int) []byte {
	if r.err != nil || len(r.b) < n {
		r.err = errShort
		return make([]byte, n)
	}
	p := r.b[:n]
	r.b = r.b[n:]
	return p
}
func (r *rd) u8() byte       { return r.take(1)[0] }
func (r *rd) u32() uint32    { return binary.BigEndian.Uint32(r.take(4)) }
func (r *rd) u64() uint64    { return binary.BigEndian.Uint64(r.take(8)) }
func (r *rd) u128() *big.Int { return new(big.Int).SetBytes(r.take(16)) }

// i128 prices are uint32 on chain, so the low 8 bytes carry them.
func (r *rd) i128() int64 { return int64(binary.BigEndian.Uint64(r.take(16)[8:])) }

// decode parses one binary socket message: 8-byte sub id + KXMD envelope.
func decode(b []byte) (*frame, error) {
	r := &rd{b: b}
	f := &frame{ID: r.u64()}
	if string(r.take(4)) != "KXMD" {
		return nil, fmt.Errorf("bad magic")
	}
	if v := r.u8(); v != 1 {
		return nil, fmt.Errorf("wire version %d", v)
	}
	f.Kind = r.u8()
	r.u8() // view
	flags := r.u8()
	f.Epoch = r.u64()
	switch f.Kind {
	case kindBook:
		f.Market = common.BytesToAddress(r.take(20)).Hex()
		f.Seq = r.u64()
		r.take(8 + 40 + 8 + 4 + 13) // globalSeq, stateHead, durationMs, maxDepth, grouping
		nb, na := r.u32(), r.u32()
		extra := 0
		if flags == 2 {
			extra = 36
		}
		lv := func(n uint32) []level {
			out := make([]level, 0, n)
			for i := uint32(0); i < n && r.err == nil; i++ {
				out = append(out, level{Px: r.i128(), Sz: r.u128()})
				r.take(extra)
			}
			return out
		}
		f.Bids, f.Asks = lv(nb), lv(na)
	case kindDelta, kindTrades:
		f.Market = common.BytesToAddress(r.take(20)).Hex()
		f.Seq, f.Prev = r.u64(), r.u64()
		r.take(8 + 40) // globalSeq, source block
		n := r.u32()
		for i := uint32(0); i < n && r.err == nil; i++ {
			if f.Kind == kindDelta {
				u := update{Buy: r.u8() == 1}
				u.Px, u.Sz = int64(r.u64()), r.u128()
				r.take(16 + 16 + 4)
				f.Update = append(f.Update, u)
				continue
			}
			r.take(8 + 2) // tradeId, recordIndex
			t := fill{Buy: r.u8() == 1}
			t.Px, t.Sz, t.Ts = int64(r.u64()), r.u128(), int64(r.u64())
			f.Fills = append(f.Fills, t)
		}
	}
	return f, r.err
}
