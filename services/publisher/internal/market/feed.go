package market

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

const (
	topicBook   = "l2Delta@f"
	topicTrades = "trades@f"
)

// Feed keeps one Kuru gateway connection subscribed to every allowed market and publishes books, trades and refs.
type Feed struct {
	WSURL, APIURL string
	RDB           redis.Cmdable
	Markets       func() []streams.MarketInfo // registry view, polled every second
	DeviationBps  int
	Tau           time.Duration

	connected atomic.Bool
	state     map[string]*mstate
}

type mstate struct {
	info      streams.MarketInfo
	book      book
	ref       ewma
	lastTrade float64 // display px
	booted    bool    // REST bootstrap attempted
	seq       uint64
}

type sub struct{ market, topic string }

type session struct {
	f      *Feed
	c      *websocket.Conn
	ctx    context.Context
	subs   map[uint64]sub
	nextID uint64
	epoch  uint64
}

func (f *Feed) Connected() bool { return f.connected.Load() }

// Run reconnects with exponential backoff until ctx ends.
func (f *Feed) Run(ctx context.Context) error {
	if f.state == nil {
		f.state = map[string]*mstate{}
	}
	backoff := time.Second
	for {
		start := time.Now()
		err := f.session(ctx)
		f.connected.Store(false)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		slog.Warn("gateway disconnected", "err", err, "retry", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

type msg struct {
	typ websocket.MessageType
	b   []byte
}

func (f *Feed) session(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c, _, err := websocket.Dial(ctx, f.WSURL, nil)
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(16 << 20)

	msgs, errc := make(chan msg, 256), make(chan error, 1)
	go func() {
		for {
			typ, b, err := c.Read(ctx)
			if err != nil {
				errc <- err
				return
			}
			select {
			case msgs <- msg{typ, b}:
			case <-ctx.Done():
				return
			}
		}
	}()
	// Ping blocks until the pong is read, so it lives off the main loop; a dead peer closes the conn.
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, pc := context.WithTimeout(ctx, 10*time.Second)
				err := c.Ping(pctx)
				pc()
				if err != nil {
					c.CloseNow()
					return
				}
			}
		}
	}()

	s := &session{f: f, c: c, ctx: ctx, subs: map[uint64]sub{}}
	for _, st := range f.state {
		st.book = book{}
	}
	if err := s.sync(); err != nil {
		return err
	}
	f.connected.Store(true)
	slog.Info("gateway connected", "markets", len(f.state))
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errc:
			return err
		case m := <-msgs:
			if err := s.handle(m); err != nil {
				return err
			}
		case <-tick.C:
			if err := s.sync(); err != nil {
				return err
			}
			f.publishRefs(ctx)
		}
	}
}

func (s *session) send(v any) error {
	b, _ := json.Marshal(v)
	return s.c.Write(s.ctx, websocket.MessageText, b)
}

func (s *session) subscribe(market, topic string) error {
	s.nextID++
	s.subs[s.nextID] = sub{market, topic}
	return s.send(map[string]any{"op": "sub", "id": s.nextID, "t": topic, "m": market})
}

// resubscribe drops a sub and opens a fresh one; l2Delta without seq restarts from a snapshot.
func (s *session) resubscribe(market, topic string) error {
	for id, x := range s.subs {
		if x.market == market && x.topic == topic {
			delete(s.subs, id)
			if err := s.send(map[string]any{"op": "unsub", "id": id}); err != nil {
				return err
			}
		}
	}
	if st := s.f.state[market]; st != nil && topic == topicBook {
		st.book = book{}
	}
	return s.subscribe(market, topic)
}

// diff returns markets in want but not have, and in have but not want.
func diff(have map[string]*mstate, want []streams.MarketInfo) (added []streams.MarketInfo, removed []string) {
	seen := map[string]bool{}
	for _, m := range want {
		k := strings.ToLower(string(m.Market))
		seen[k] = true
		if have[k] == nil {
			added = append(added, m)
		}
	}
	for k := range have {
		if !seen[k] {
			removed = append(removed, k)
		}
	}
	return added, removed
}

// sync subscribes newly allowed markets and drops disallowed ones.
func (s *session) sync() error {
	f := s.f
	added, removed := diff(f.state, f.Markets())
	subscribed := map[string]bool{}
	for _, x := range s.subs {
		subscribed[x.market] = true
	}
	for _, m := range added {
		k := strings.ToLower(string(m.Market))
		f.state[k] = &mstate{info: m, seq: uint64(time.Now().UnixMilli())}
	}
	for k := range f.state {
		if !subscribed[k] {
			if err := s.subscribe(k, topicBook); err != nil {
				return err
			}
			if err := s.subscribe(k, topicTrades); err != nil {
				return err
			}
		}
	}
	for _, k := range removed {
		delete(f.state, k)
		for id, x := range s.subs {
			if x.market == k {
				delete(s.subs, id)
				if err := s.send(map[string]any{"op": "unsub", "id": id}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type control struct {
	Op  string `json:"op"`
	ID  uint64 `json:"id"`
	T   string `json:"t"`
	M   string `json:"m"`
	C   string `json:"c"`
	Msg string `json:"msg"`
	R   string `json:"r"`
}

func (s *session) handle(m msg) error {
	if m.typ == websocket.MessageText {
		var c control
		if err := json.Unmarshal(m.b, &c); err != nil {
			return fmt.Errorf("control %q: %w", m.b, err)
		}
		switch c.Op {
		case "err":
			slog.Warn("gateway error", "id", c.ID, "code", c.C, "msg", c.Msg, "sub", s.subs[c.ID])
		case "resync":
			slog.Info("gateway resync", "t", c.T, "m", c.M)
			k := strings.ToLower(c.M)
			if s.f.state[k] != nil {
				return s.resubscribe(k, strings.SplitN(c.T, "@", 2)[0]+"@f")
			}
		case "resub":
			if x, ok := s.subs[c.ID]; ok {
				slog.Info("gateway resub", "sub", x, "r", c.R)
				return s.resubscribe(x.market, x.topic)
			}
		}
		return nil
	}
	fr, err := decode(m.b)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	x, ok := s.subs[fr.ID]
	st := s.f.state[x.market]
	if !ok || st == nil {
		return nil
	}
	if s.epoch == 0 {
		s.epoch = fr.Epoch
	}
	if fr.Epoch != s.epoch {
		return fmt.Errorf("feed epoch %d -> %d", s.epoch, fr.Epoch)
	}
	switch fr.Kind {
	case kindBook:
		st.book.snapshot(fr)
		s.f.publishBook(s.ctx, st)
	case kindDelta:
		changed, gap := st.book.apply(fr)
		if gap {
			slog.Info("book seq gap, resync", "market", x.market, "seq", fr.Seq, "prev", fr.Prev)
			return s.resubscribe(x.market, topicBook)
		}
		if changed {
			s.f.publishBook(s.ctx, st)
		}
	case kindTrades:
		s.f.publishTrades(s.ctx, st, fr.Fills)
	}
	return nil
}

func (f *Feed) publishBook(ctx context.Context, st *mstate) {
	st.seq++ // ponytail: seeded from unix ms so it stays monotonic across restarts at < 1000 publishes/s
	bids, asks := st.book.levels(st.info)
	snap := streams.BookSnapshot{Market: st.info.Market, Seq: st.seq, Ts: time.Now().UnixMilli(), Bids: bids, Asks: asks}
	if err := streams.Publish(ctx, f.RDB, streams.Book(string(st.info.Market)), streams.BookLatest(string(st.info.Market)), snap); err != nil {
		slog.Error("publish book", "market", st.info.Market, "err", err)
	}
}

func (f *Feed) publishTrades(ctx context.Context, st *mstate, fills []fill) {
	for _, x := range fills {
		px := decimal.NewFromInt(x.Px).Div(st.info.PricePrecision)
		side := "sell"
		if x.Buy {
			side = "buy"
		}
		t := streams.Trade{Market: st.info.Market, Ts: x.Ts * 1000, Px: px, Sz: decimal.NewFromBigInt(x.Sz, 0).Div(st.info.SizePrecision), Side: side}
		if err := streams.Publish(ctx, f.RDB, streams.Trades(string(st.info.Market)), "", t); err != nil {
			slog.Error("publish trade", "market", st.info.Market, "err", err)
		}
		st.lastTrade = px.InexactFloat64()
	}
}

// mid is the display mid, 0 when either side is empty.
func (st *mstate) mid() float64 {
	var bid, ask int64
	for p := range st.book.bids {
		bid = max(bid, p)
	}
	for p := range st.book.asks {
		if ask == 0 || p < ask {
			ask = p
		}
	}
	if bid == 0 || ask == 0 {
		return 0
	}
	return float64(bid+ask) / 2 / st.info.PricePrecision.InexactFloat64()
}

func (f *Feed) publishRefs(ctx context.Context) {
	now := time.Now()
	for k, st := range f.state {
		if !st.book.synced {
			continue
		}
		mid := st.mid()
		x := mid
		if x == 0 {
			if st.lastTrade == 0 && st.ref.ref == 0 && !st.booted {
				st.booted = true
				px, err := f.lastTradePx(ctx, k, st.info.PricePrecision)
				if err != nil {
					slog.Warn("ref bootstrap", "market", k, "err", err)
				}
				st.lastTrade = px
			}
			x = st.lastTrade
		}
		st.ref.observe(x, now, f.Tau)
		if st.ref.ref == 0 {
			continue
		}
		r := streams.RefPrice{
			Market: st.info.Market, Mid: decimal.NewFromFloat(mid), Ref: decimal.NewFromFloat(st.ref.ref),
			Vol: decimal.NewFromFloat(st.ref.vol), Ts: now.UnixMilli(), Deviated: deviated(mid, st.ref.ref, f.DeviationBps),
		}
		if err := streams.Publish(ctx, f.RDB, "", streams.Ref(k), r); err != nil {
			slog.Error("publish ref", "market", k, "err", err)
		}
	}
}

var httpc = &http.Client{Timeout: 5 * time.Second}

// lastTradePx reads the latest Kuru trade over REST (kuru-spot-v2.md §7.2); 0 when the market never traded.
func (f *Feed) lastTradePx(ctx context.Context, market string, pp decimal.Decimal) (float64, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.APIURL+"/api/v1/markets/"+market+"/trades?limit=1", nil)
	res, err := httpc.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %s", res.Status)
	}
	var body struct {
		Data []struct {
			Price string `json:"price"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || len(body.Data) == 0 {
		return 0, err
	}
	p, err := decimal.NewFromString(body.Data[0].Price)
	if err != nil {
		return 0, err
	}
	return p.Div(pp).InexactFloat64(), nil
}
