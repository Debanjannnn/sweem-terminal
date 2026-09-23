package kuru

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/wad"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/book"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/candle"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/ws"
)

// Topic views, decided per stream rather than globally.
//
// The book is what the UI quotes and trades against, so it takes the lowest
// latency Monad offers: proposed, plus the branchDropped rollback that tier
// obliges us to handle. Trades become ticks, candles and postgres rows, so they
// take finalized -- there is no rolling back a row we already wrote.
const (
	bookView  = finality.Seen
	tradeView = finality.Final

	topicBook   = "l2Delta"
	topicTrades = "trades"

	// the native ping is absorbed by the codec and never reaches this loop, so
	// the JSON ping is what keeps a quiet market inside the read deadline
	gwReadTimeout  = 60 * time.Second
	gwPingEvery    = 20 * time.Second
	healthySession = 5 * time.Minute
)

var backoffs = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute}

type Handlers struct {
	Book func(book.Book)
	Tick func(candle.Tick)
}

// Stream owns the gateway socket and runs until ctx is cancelled. One
// connection multiplexes every market.
func (c *Client) Stream(ctx context.Context, h Handlers) error {
	attempt := 0
	for ctx.Err() == nil {
		started := time.Now()
		err := c.session(ctx, h)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(started) > healthySession {
			attempt = 0
		}
		delay := backoffs[min(attempt, len(backoffs)-1)]
		attempt++
		slog.Warn("kuru gateway socket dropped", "code", ws.CloseCode(err), "err", err, "retry_in", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return ctx.Err()
}

type gwSub struct {
	topic   string
	view    finality.Tier
	market  store.Market
	seq     uint64 // last applied marketSeq; 0 means no cursor
	bridged bool
}

func (s *gwSub) name() string { return s.topic + viewSuffix(s.view) }

// advance enforces the replay continuity rule. Streams are sparse, so the
// predecessor is never seq-1: the first frame after a cursor only has to land
// beyond it, every later one must name the previous frame exactly.
func (s *gwSub) advance(seq, prev uint64) bool {
	if s.bridged {
		if prev != s.seq {
			return false
		}
	} else if s.seq != 0 && (seq <= s.seq || prev > s.seq) {
		return false
	}
	s.bridged, s.seq = true, seq
	return true
}

type gwSession struct {
	c     *Client
	conn  *ws.Conn
	h     Handlers
	epoch uint64
	next  uint64
	subs  map[uint64]*gwSub
	books map[string]*book.Book
}

func (c *Client) session(ctx context.Context, h Handlers) error {
	markets := c.Cached()
	if len(markets) == 0 {
		var err error
		if markets, err = c.Markets(ctx); err != nil {
			return err
		}
	}
	conn, err := ws.Dial(c.gatewayWS, 15*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()

	go keepalive(conn, stop)

	// ponytail: cursors live and die with the session, so a reconnect rejoins
	// trades live; carry (feedEpoch, marketSeq) across sessions if a gap of
	// prints during a reconnect ever matters more than the indexer backfill
	s := &gwSession{c: c, conn: conn, h: h, subs: map[uint64]*gwSub{}, books: map[string]*book.Book{}}
	for _, m := range markets {
		if b, err := c.Book(ctx, m.ID, DefaultDepth); err == nil {
			s.books[m.ID] = &b
			s.publish(m.ID)
		}
		if err := s.subscribe(&gwSub{topic: topicBook, view: bookView, market: m}); err != nil {
			return err
		}
		if err := s.subscribe(&gwSub{topic: topicTrades, view: tradeView, market: m}); err != nil {
			return err
		}
	}
	for {
		conn.NetConn().SetReadDeadline(time.Now().Add(gwReadTimeout))
		op, msg, err := conn.Read()
		if err != nil {
			return err
		}
		if op == ws.OpText {
			err = s.control(msg)
		} else {
			err = s.frame(msg)
		}
		if err != nil {
			return err
		}
	}
}

// keepalive earns a pong, which is the only traffic a quiet market guarantees
// inside the read deadline.
func keepalive(conn *ws.Conn, stop <-chan struct{}) {
	t := time.NewTicker(gwPingEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			b, _ := json.Marshal(map[string]any{"op": "ping", "ts": now.UnixMilli()})
			if conn.WriteText(b) != nil {
				return
			}
		}
	}
}

type subReq struct {
	Op        string `json:"op"`
	ID        uint64 `json:"id"`
	T         string `json:"t,omitempty"`
	M         string `json:"m,omitempty"`
	FeedEpoch uint64 `json:"feedEpoch,omitempty"`
	Seq       uint64 `json:"seq,omitempty"`
}

// subscribe installs a sub under a fresh id. Replaying from a cursor needs the
// epoch too: sequences are namespaced by it and never comparable across one.
func (s *gwSession) subscribe(sub *gwSub) error {
	s.next++
	id := s.next
	sub.bridged = false
	req := subReq{Op: "sub", ID: id, T: sub.name(), M: sub.market.Address, Seq: sub.seq}
	if req.Seq != 0 {
		req.FeedEpoch = s.epoch
	}
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := s.conn.WriteText(b); err != nil {
		return err
	}
	s.subs[id] = sub
	return nil
}

func (s *gwSession) resubscribe(id uint64, sub *gwSub, keepCursor bool) error {
	delete(s.subs, id)
	b, err := json.Marshal(subReq{Op: "unsub", ID: id})
	if err != nil {
		return err
	}
	if err := s.conn.WriteText(b); err != nil {
		return err
	}
	if !keepCursor {
		sub.seq = 0
		delete(s.books, sub.market.ID)
	}
	return s.subscribe(sub)
}

type ctrlMsg struct {
	Op        string  `json:"op"`
	ID        *uint64 `json:"id"`
	T         string  `json:"t"`
	M         string  `json:"m"`
	C         string  `json:"c"`
	Msg       string  `json:"msg"`
	R         string  `json:"r"`
	Seq       uint64  `json:"seq"`
	FeedEpoch uint64  `json:"feedEpoch"`
}

func (s *gwSession) control(msg []byte) error {
	var m ctrlMsg
	if err := json.Unmarshal(msg, &m); err != nil {
		return fmt.Errorf("kuru gateway sent a control frame that is not JSON: %w", err)
	}
	switch m.Op {
	case "ack":
		s.epoch = m.FeedEpoch
	case "err":
		slog.Warn("kuru gateway rejected a request", "code", m.C, "msg", m.Msg, "topic", m.T)
		if m.ID != nil {
			delete(s.subs, *m.ID)
		}
	case "resync":
		return s.onResync(m)
	case "resub":
		if m.ID == nil {
			return nil
		}
		sub, ok := s.subs[*m.ID]
		if !ok {
			return nil
		}
		slog.Info("kuru gateway asked for a resub", "topic", m.T, "reason", m.R)
		return s.resubscribe(*m.ID, sub, true)
	}
	return nil
}

// A resync without an id is a market-scoped gap: the gateway names the topic
// and market instead. Either way the derived state is gone.
func (s *gwSession) onResync(m ctrlMsg) error {
	id, sub := s.find(m)
	if sub == nil {
		return nil
	}
	slog.Info("kuru gateway asked for a resync", "topic", m.T, "reason", m.R, "seq", m.Seq)
	if sub.topic == topicTrades && m.Seq != 0 {
		sub.seq = m.Seq
		return s.resubscribe(id, sub, true)
	}
	return s.resubscribe(id, sub, false)
}

func (s *gwSession) find(m ctrlMsg) (uint64, *gwSub) {
	if m.ID != nil {
		return *m.ID, s.subs[*m.ID]
	}
	for id, sub := range s.subs {
		if sub.name() == m.T && sub.market.Address == m.M {
			return id, sub
		}
	}
	return 0, nil
}

func (s *gwSession) frame(msg []byte) error {
	env, err := DecodeEnvelope(msg)
	if err != nil {
		return err
	}
	sub, ok := s.subs[env.SubID]
	if !ok {
		return nil // a frame still in flight for a subscription we dropped
	}
	if s.epoch != 0 && env.FeedEpoch != s.epoch {
		// sequences are namespaced by the epoch, so every cursor we hold is
		// now meaningless: drop the connection and bootstrap from scratch
		return fmt.Errorf("kuru gateway feed epoch moved %d -> %d", s.epoch, env.FeedEpoch)
	}
	v, err := env.Decode()
	if err != nil {
		return err
	}
	switch f := v.(type) {
	case L2Book:
		sub.bridged, sub.seq = true, f.Seq
		s.books[sub.market.ID] = &book.Book{
			MarketID: sub.market.ID, Venue: sub.market.Venue,
			Bids: levels(f.Bids, sub.market, true),
			Asks: levels(f.Asks, sub.market, false),
		}
		slog.Debug("kuru book bootstrapped", "market", sub.market.ID, "seq", f.Seq,
			"block", f.Head.Number, "bids", len(f.Bids), "asks", len(f.Asks))
		s.publish(sub.market.ID)
	case L2Delta:
		if !sub.advance(f.Seq, f.PrevSeq) {
			return s.gap(env.SubID, sub, f.Seq, f.PrevSeq)
		}
		s.applyDelta(sub, f)
	case Trades:
		if !sub.advance(f.Seq, f.PrevSeq) {
			return s.gap(env.SubID, sub, f.Seq, f.PrevSeq)
		}
		s.emitTrades(sub, f)
	case Lifecycle:
		return s.lifecycle(env.SubID, sub, f)
	}
	return nil
}

func (s *gwSession) gap(id uint64, sub *gwSub, seq, prev uint64) error {
	slog.Warn("kuru gateway sequence gap", "topic", sub.name(), "market", sub.market.ID,
		"have", sub.seq, "frame_seq", seq, "frame_prev", prev)
	// trades replay from the cursor and close the hole; a book is cheaper to re-bootstrap
	return s.resubscribe(id, sub, sub.topic == topicTrades)
}

// A finalized market stream emits no lifecycle frames, but a finalized user
// stream can still replay promoted ones, so this branches on kind and not on
// the view we asked for.
func (s *gwSession) lifecycle(id uint64, sub *gwSub, f Lifecycle) error {
	if f.Market != nil && !sub.advance(f.Market.Seq, f.Market.PrevSeq) {
		return s.gap(id, sub, f.Market.Seq, f.Market.PrevSeq)
	}
	if f.Action != BranchDropped {
		return nil
	}
	slog.Warn("kuru branch dropped, rolling back", "market", sub.market.ID,
		"block", f.Block.Number, "reason", f.DropReason, "replacement", f.ReplacementID)
	if sub.view == finality.Final {
		return nil // nothing derived from a finalized frame is ours to rewind
	}
	// ponytail: rollback is drop-and-rebootstrap, add a per-block undo log if
	// the snapshot round trip ever shows up as a visible book stall
	return s.resubscribe(id, sub, false)
}

func (s *gwSession) applyDelta(sub *gwSub, d L2Delta) {
	b := s.books[sub.market.ID]
	if b == nil {
		// the gateway snapshots before it deltas, so this cannot happen
		// without the stream having lied about its own ordering
		slog.Warn("kuru delta with no snapshot under it", "market", sub.market.ID, "seq", d.Seq)
		return
	}
	var bids, asks []book.Level
	for _, u := range d.Updates {
		l := wadLevel(u.Price, u.TotalBase, sub.market)
		if u.Side == SideBuy {
			bids = append(bids, l)
		} else {
			asks = append(asks, l)
		}
	}
	b.Apply(bids, asks)
	slog.Debug("kuru l2 delta", "market", sub.market.ID, "seq", d.Seq, "updates", len(d.Updates))
	s.publish(sub.market.ID)
}

func (s *gwSession) publish(id string) {
	b := s.books[id]
	if b == nil || s.h.Book == nil {
		return
	}
	b.Ts = time.Now().UnixMilli()
	s.h.Book(*b)
}

func (s *gwSession) emitTrades(sub *gwSub, t Trades) {
	if s.h.Tick == nil {
		return
	}
	for _, row := range t.Rows {
		side := "buy"
		if row.TakerSide == SideSell {
			side = "sell"
		}
		l := wadLevel(row.Price, row.BaseFilled, sub.market)
		s.h.Tick(candle.Tick{
			MarketID: sub.market.ID, Venue: sub.market.Venue, Side: side,
			Price: l.Price, Size: l.Size, Ts: int64(row.Ts) * 1000,
		})
	}
}

func wadLevel(price, size *big.Int, m store.Market) book.Level {
	return book.Level{
		Price: wad.Scale(price, m.PriceDecimals, wad.Decimals).String(),
		Size:  wad.Scale(size, m.SizeDecimals, wad.Decimals).String(),
	}
}

func levels(ls []PriceLevel, m store.Market, desc bool) []book.Level {
	out := make([]book.Level, 0, len(ls))
	for _, l := range ls {
		if l.Price.Sign() <= 0 || l.TotalBase.Sign() <= 0 {
			continue
		}
		out = append(out, wadLevel(l.Price, l.TotalBase, m))
	}
	return book.Sort(out, desc)
}
