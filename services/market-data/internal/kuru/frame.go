package kuru

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
)

// Gateway binary frame = 8-byte BE subscription id + a KXMD envelope. Every
// integer is big-endian and every value is native: prices are pp integers,
// sizes are size-precision integers, balances are token atoms. The decoder
// scales nothing.
//
// A zero sequence means null on the wire. We keep it as 0 because every real
// cursor is > 0, so the "previous is null or <= cursor" replay rule holds
// unchanged.

type Kind uint8

const (
	KindL2Book Kind = iota + 1
	KindL2Delta
	KindTrades
	KindLifecycle
	KindBBO
	KindAllMids
	KindUserOrders
	KindUserBalances
	KindUserTrades
)

var kindNames = [...]string{"", "l2Book", "l2Delta", "trades", "lifecycle", "bbo", "allMids", "userOrders", "userBalances", "userTrades"}

func (k Kind) String() string {
	if int(k) >= len(kindNames) || k == 0 {
		return fmt.Sprintf("kind(%d)", uint8(k))
	}
	return kindNames[k]
}

// lifecycle actions
const (
	BlockProposed uint8 = iota + 1
	BlockVoted
	BlockFinalized
	BlockExecuted
	BranchDropped
)

// l2Delta / trades side codes
const (
	SideBuy  uint8 = 1
	SideSell uint8 = 2
)

const wireVersion = 1

// viewTier maps the gateway's view code onto Monad's three block tiers, which
// is the same seen/safe/final model the rest of the stack already speaks.
func viewTier(code uint8) (finality.Tier, error) {
	switch code {
	case 1:
		return finality.Seen, nil
	case 2:
		return finality.Safe, nil
	case 3:
		return finality.Final, nil
	}
	return 0, fmt.Errorf("view code %d is not proposed, voted or finalized", code)
}

func viewSuffix(t finality.Tier) string {
	switch t {
	case finality.Safe:
		return "@v"
	case finality.Final:
		return "@f"
	default:
		return "@p"
	}
}

func viewState(t finality.Tier) string {
	switch t {
	case finality.Safe:
		return "voted"
	case finality.Final:
		return "finalized"
	default:
		return "proposed"
	}
}

type Envelope struct {
	SubID     uint64
	Kind      Kind
	View      finality.Tier
	Flags     uint8
	FeedEpoch uint64
	body      []byte
}

func DecodeEnvelope(msg []byte) (Envelope, error) {
	if len(msg) < 24 {
		return Envelope{}, fmt.Errorf("gateway frame is %d bytes, too short for an envelope", len(msg))
	}
	if string(msg[8:12]) != "KXMD" {
		return Envelope{}, fmt.Errorf("gateway frame has magic %q, not KXMD", msg[8:12])
	}
	if msg[12] != wireVersion {
		return Envelope{}, fmt.Errorf("gateway wire version %d is not %d, so every offset below is untrustworthy", msg[12], wireVersion)
	}
	view, err := viewTier(msg[14])
	if err != nil {
		return Envelope{}, err
	}
	e := Envelope{
		SubID:     binary.BigEndian.Uint64(msg[:8]),
		Kind:      Kind(msg[13]),
		View:      view,
		Flags:     msg[15],
		FeedEpoch: binary.BigEndian.Uint64(msg[16:24]),
		body:      msg[24:],
	}
	return e, e.checkFlags()
}

func (e Envelope) checkFlags() error {
	switch e.Kind {
	case KindL2Book:
		if e.Flags != 1 && e.Flags != 2 {
			return fmt.Errorf("l2Book flags %d is neither compact nor extended", e.Flags)
		}
	case KindUserOrders, KindUserBalances:
		if e.Flags > 1 {
			return fmt.Errorf("%s flags %d is neither delta nor snapshot", e.Kind, e.Flags)
		}
	default:
		if e.Flags != 0 {
			return fmt.Errorf("%s carries flags %d, which must be zero", e.Kind, e.Flags)
		}
	}
	return nil
}

func (e Envelope) Snapshot() bool { return e.Flags == 1 }

type MarketRef struct {
	Market    string
	Seq       uint64
	PrevSeq   uint64
	GlobalSeq uint64
}

type UserRef struct {
	UserID    uint64
	Seq       uint64
	GlobalSeq uint64
	PrevSeq   uint64
}

type Block struct {
	Number uint64
	ID     string
}

type PriceLevel struct {
	Price       *big.Int
	TotalBase   *big.Int
	ActiveBase  *big.Int
	PassiveBase *big.Int
	Orders      uint32
}

type L2Book struct {
	MarketRef
	Head       Block
	DurationMs uint64
	MaxDepth   uint32
	Bids, Asks []PriceLevel
}

type L2Delta struct {
	MarketRef
	Source  Block
	Updates []L2Update
}

type L2Update struct {
	Side uint8
	PriceLevel
}

type Trades struct {
	MarketRef
	Source Block
	Rows   []Trade
}

type Trade struct {
	ID         uint64
	RecordIdx  uint16
	TakerSide  uint8
	Price      *big.Int
	BaseFilled *big.Int
	Ts         uint64
}

type Lifecycle struct {
	Market        *MarketRef
	User          *UserRef
	Action        uint8
	Block         Block
	ParentID      string
	DropReason    uint8
	ReplacementID string
}

type BBO struct {
	MarketRef
	Head     Block
	Bid, Ask *PriceLevel
}

type AllMids struct{ Mids []Mid }

type Mid struct {
	Market string
	Price  *big.Int
}

type UserOrders struct {
	UserRef
	Block  Block
	Orders []UserOrder  // snapshot only
	Events []OrderEvent // delta only
}

type UserOrder struct {
	Market            string
	OrderID           uint64
	SlotIdx           uint8
	Side              uint8
	Price             *big.Int
	RemainingBase     *big.Int
	MinSizeAfterBlock uint64
	ClientOrderID     string
	CreatedAt         uint64
}

// OrderEvent carries the only fields the wire spec pins down per code; the
// per-code payload is sized but its field layout is not published.
type OrderEvent struct {
	Code      uint8
	TxHash    string
	TxIdx     uint32
	LogIdx    uint32
	RecordIdx uint16
	MakerID   uint64
	Ts        uint64
	Payload   []byte
}

type UserBalances struct {
	UserRef
	Block    Block
	Balances []Balance
}

type Balance struct {
	Token    string
	Free     *big.Int
	Reserved *big.Int
}

type UserTrades struct {
	UserRef
	Source Block
	Rows   []UserTrade
}

type UserTrade struct {
	Market                string
	TradeID               uint64
	RecordIdx             uint16
	TakerID, MakerID      uint64
	TakerSide             uint8
	Price, BaseFilled     *big.Int
	Liquidity             uint8 // 1 activeFifo, 2 passiveBand
	SlotIdx               uint8
	OrderID               uint64
	MakerSide             uint8
	RemainingBaseAfter    *big.Int
	MakerFeePps           uint32
	LowPrice              *big.Int
	PassiveRemainingAfter *big.Int
	TxHash                string
	TxIdx, LogIdx         uint32
	TakerFeePps           uint32
	BuilderFeePps         uint32
	MatchEnd              uint8
	Ts                    uint64
}

// Decode returns the kind-specific frame. Unknown kinds, reserved bits and
// trailing bytes all fail closed: once one offset is wrong every later one is.
func (e Envelope) Decode() (any, error) {
	r := &rd{b: e.body}
	var v any
	switch e.Kind {
	case KindL2Book:
		v = readL2Book(r, e.Flags)
	case KindL2Delta:
		v = readL2Delta(r)
	case KindTrades:
		v = readTrades(r)
	case KindLifecycle:
		v = readLifecycle(r)
	case KindBBO:
		v = readBBO(r)
	case KindAllMids:
		v = readAllMids(r)
	case KindUserOrders:
		v = readUserOrders(r, e.Snapshot())
	case KindUserBalances:
		v = readUserBalances(r, e.Snapshot())
	case KindUserTrades:
		v = readUserTrades(r)
	default:
		return nil, fmt.Errorf("gateway sent %s, which this decoder does not speak", e.Kind)
	}
	if err := r.end(); err != nil {
		return nil, fmt.Errorf("%s: %w", e.Kind, err)
	}
	return v, nil
}

func readMarketRef(r *rd, replayable bool) MarketRef {
	m := MarketRef{Market: r.addr(), Seq: r.u64()}
	if replayable {
		m.PrevSeq = r.u64()
	}
	m.GlobalSeq = r.u64()
	return m
}

func readUserRef(r *rd) UserRef {
	return UserRef{UserID: r.u64(), Seq: r.u64(), GlobalSeq: r.u64(), PrevSeq: r.u64()}
}

func readBlock(r *rd) Block { return Block{Number: r.u64(), ID: r.hash()} }

func readOptionalBlock(r *rd) Block {
	present := r.u8()
	b := readBlock(r)
	if present == 0 {
		return Block{}
	}
	return b
}

func readL2Book(r *rd, flags uint8) L2Book {
	b := L2Book{MarketRef: readMarketRef(r, false), Head: readBlock(r), DurationMs: r.u64(), MaxDepth: r.u32()}
	r.skip(13) // grouping: code, nativeTickSize, figures, mantissa, reserved
	size, extended := 32, flags == 2
	if extended {
		size = 68
	}
	nb, na := int(r.u32()), int(r.u32())
	r.fits(nb+na, size)
	b.Bids = readLevels(r, nb, extended)
	b.Asks = readLevels(r, na, extended)
	return b
}

func readLevels(r *rd, n int, extended bool) []PriceLevel {
	out := make([]PriceLevel, 0, n)
	for range n {
		l := PriceLevel{Price: r.int(16), TotalBase: r.uint(16)}
		if extended {
			l.ActiveBase, l.PassiveBase, l.Orders = r.uint(16), r.uint(16), r.u32()
		}
		out = append(out, l)
	}
	return out
}

func readL2Delta(r *rd) L2Delta {
	d := L2Delta{MarketRef: readMarketRef(r, true), Source: readBlock(r)}
	n := int(r.u32())
	r.fits(n, 61)
	for range n {
		d.Updates = append(d.Updates, L2Update{
			Side: r.u8(),
			PriceLevel: PriceLevel{
				Price: r.int(8), TotalBase: r.uint(16),
				ActiveBase: r.uint(16), PassiveBase: r.uint(16), Orders: r.u32(),
			},
		})
	}
	return d
}

func readTrades(r *rd) Trades {
	t := Trades{MarketRef: readMarketRef(r, true), Source: readBlock(r)}
	n := int(r.u32())
	r.fits(n, 43)
	for range n {
		t.Rows = append(t.Rows, Trade{
			ID: r.u64(), RecordIdx: r.u16(), TakerSide: r.u8(),
			Price: r.int(8), BaseFilled: r.uint(16), Ts: r.u64(),
		})
	}
	return t
}

// scope is carried by body length alone: 156 bytes market, 144 user.
func readLifecycle(r *rd) Lifecycle {
	var l Lifecycle
	switch len(r.b) {
	case 156:
		m := readMarketRef(r, true)
		l.Market = &m
	case 144:
		u := readUserRef(r)
		l.User = &u
	default:
		r.fail("lifecycle body is %d bytes, neither the 156-byte market nor the 144-byte user scope", len(r.b))
		return l
	}
	l.Action = r.u8()
	r.reserved(3)
	l.Block = readBlock(r)
	l.ParentID = r.hash()
	l.DropReason = r.u8()
	present := r.u8()
	r.reserved(2)
	l.ReplacementID = r.hash()
	if present == 0 {
		l.ReplacementID = ""
	}
	return l
}

func readBBO(r *rd) BBO {
	b := BBO{MarketRef: readMarketRef(r, false), Head: readBlock(r)}
	b.Bid, b.Ask = readSide(r), readSide(r)
	return b
}

// an absent side is zero-filled, which is not the same as a zero-size level
func readSide(r *rd) *PriceLevel {
	present := r.u8()
	l := PriceLevel{Price: r.int(16), TotalBase: r.uint(16)}
	if present == 0 {
		return nil
	}
	return &l
}

func readAllMids(r *rd) AllMids {
	n := int(r.u32())
	r.fits(n, 36)
	var a AllMids
	for range n {
		a.Mids = append(a.Mids, Mid{Market: r.addr(), Price: r.int(16)})
	}
	return a
}

func readUserOrders(r *rd, snapshot bool) UserOrders {
	o := UserOrders{UserRef: readUserRef(r)}
	if snapshot {
		o.Block = readOptionalBlock(r)
	} else {
		o.Block = readBlock(r)
	}
	n := int(r.u32())
	if snapshot {
		r.fits(n, 104)
		for range n {
			u := UserOrder{
				Market: r.addr(), OrderID: r.u64(), SlotIdx: r.u8(), Side: r.u8(),
				Price: r.int(8), RemainingBase: r.uint(16),
			}
			hasMin := r.u8()
			u.MinSizeAfterBlock = r.u64()
			if hasMin == 0 {
				u.MinSizeAfterBlock = 0
			}
			hasCOID := r.u8()
			u.ClientOrderID = r.hash()
			if hasCOID == 0 {
				u.ClientOrderID = ""
			}
			u.CreatedAt = r.u64()
			o.Orders = append(o.Orders, u)
		}
		r.reserved(4) // removal count, must be zero in a snapshot
		return o
	}
	for range n {
		e := OrderEvent{Code: r.u8(), TxHash: r.hash(), TxIdx: r.u32(), LogIdx: r.u32(), RecordIdx: r.u16()}
		switch e.Code {
		case 1:
			e.MakerID = r.u64()
			e.Payload = r.take(96)
			e.Ts = r.u64()
		case 2:
			e.Payload = r.take(93)
		case 3:
			e.Payload = r.take(45)
		case 4:
			e.Payload = r.take(61)
		default:
			r.fail("userOrders delta code %d is not created, trade, cancelled or rab-reduced", e.Code)
			return o
		}
		o.Events = append(o.Events, e)
	}
	return o
}

func readUserBalances(r *rd, snapshot bool) UserBalances {
	b := UserBalances{UserRef: readUserRef(r)}
	if snapshot {
		b.Block = readOptionalBlock(r)
	} else {
		b.Block = readBlock(r)
	}
	n := int(r.u32())
	r.fits(n, 64)
	for range n {
		r.reserved(12)
		b.Balances = append(b.Balances, Balance{Token: r.addr(), Free: r.uint(16), Reserved: r.uint(16)})
	}
	return b
}

func readUserTrades(r *rd) UserTrades {
	t := UserTrades{UserRef: readUserRef(r), Source: readBlock(r)}
	n := int(r.u32())
	r.fits(n, 153) // rows are 159 or 153; the smaller one bounds the allocation
	for range n {
		u := UserTrade{
			Market: r.addr(), TradeID: r.u64(), RecordIdx: r.u16(),
			TakerID: r.u64(), MakerID: r.u64(), TakerSide: r.u8(),
			Price: r.int(8), BaseFilled: r.uint(16),
		}
		u.Liquidity = r.u8()
		switch u.Liquidity {
		case 1:
			u.SlotIdx, u.OrderID, u.MakerSide = r.u8(), r.u64(), r.u8()
			u.RemainingBaseAfter, u.MakerFeePps = r.uint(16), r.u32()
		case 2:
			u.LowPrice, u.PassiveRemainingAfter = r.int(8), r.uint(16)
		default:
			r.fail("userTrades liquidity code %d is neither activeFifo nor passiveBand", u.Liquidity)
			return t
		}
		u.TxHash, u.TxIdx, u.LogIdx = r.hash(), r.u32(), r.u32()
		u.TakerFeePps, u.BuilderFeePps, u.MatchEnd, u.Ts = r.u32(), r.u32(), r.u8(), r.u64()
		t.Rows = append(t.Rows, u)
	}
	return t
}

// rd is a fail-closed big-endian cursor: the first short read poisons every
// later one, and end() rejects leftovers.
type rd struct {
	b   []byte
	i   int
	err error
}

func (r *rd) fail(format string, a ...any) {
	if r.err == nil {
		r.err = fmt.Errorf(format, a...)
	}
}

func (r *rd) take(n int) []byte {
	if r.err != nil || r.i+n > len(r.b) {
		r.fail("frame is %d bytes, too short for %d more at offset %d", len(r.b), n, r.i)
		return make([]byte, n)
	}
	v := r.b[r.i : r.i+n]
	r.i += n
	return v
}

func (r *rd) skip(n int)   { r.take(n) }
func (r *rd) u8() uint8    { return r.take(1)[0] }
func (r *rd) u16() uint16  { return binary.BigEndian.Uint16(r.take(2)) }
func (r *rd) u32() uint32  { return binary.BigEndian.Uint32(r.take(4)) }
func (r *rd) u64() uint64  { return binary.BigEndian.Uint64(r.take(8)) }
func (r *rd) addr() string { return "0x" + hex.EncodeToString(r.take(20)) }

func (r *rd) uint(n int) *big.Int { return new(big.Int).SetBytes(r.take(n)) }

func (r *rd) int(n int) *big.Int {
	v := new(big.Int).SetBytes(r.take(n))
	if v.Bit(n*8-1) == 1 {
		v.Sub(v, new(big.Int).Lsh(big.NewInt(1), uint(n*8)))
	}
	return v
}

// an all-zero hash is the wire's null
func (r *rd) hash() string {
	b := r.take(32)
	for _, x := range b {
		if x != 0 {
			return "0x" + hex.EncodeToString(b)
		}
	}
	return ""
}

func (r *rd) reserved(n int) {
	for _, x := range r.take(n) {
		if x != 0 {
			r.fail("reserved byte at offset %d is not zero", r.i-n)
			return
		}
	}
}

// fits rejects a row count that cannot be backed by the bytes left, before it
// becomes an allocation.
func (r *rd) fits(n, size int) {
	if n < 0 || n*size > len(r.b)-r.i {
		r.fail("frame claims %d rows of %d bytes but only %d remain", n, size, len(r.b)-r.i)
		r.i = len(r.b)
	}
}

func (r *rd) end() error {
	if r.err != nil {
		return r.err
	}
	if r.i != len(r.b) {
		return fmt.Errorf("frame has %d trailing bytes after offset %d", len(r.b)-r.i, r.i)
	}
	return nil
}
