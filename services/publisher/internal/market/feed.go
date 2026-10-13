package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

const (
	depth   = 20
	idleMax = 30 * time.Second // depth arrives ~3/s per busy market; silence this long on the socket = dead
	// quiet books (AUSD pairs) get no depth diffs; while the socket is live their book is still current,
	// so republish it before market-data's 30s bookMaxAge marks it stale and refuses quotes
	bookRefresh = 10 * time.Second
	dedupTTL    = time.Hour
	priceExp    = 18 // Kuru WS/REST prices are 1e18 fixed point
)

// Feed keeps one Kuru mainnet WS connection subscribed to every market and publishes books, trades, refs and candles.
type Feed struct {
	WSURL, APIURL string // wss://exchange.kuru.io/ws, https://exchange.kuru.io
	RDB           redis.Cmdable
	Markets       []streams.MarketInfo // Symbol must be set (Resolve)
	DeviationBps  int
	Tau           time.Duration
	Candles       *Candles // nil: no candles

	connected atomic.Bool
	state     map[string]*mstate
	seen      map[string]int // trade key → occurrences this session
}

type mstate struct {
	info       streams.MarketInfo
	bids, asks []streams.Level // display, top 20
	ref        ewma
	lastTrade  float64
	booted     bool
	seq        uint64
	pubAt      time.Time
}

func (f *Feed) Connected() bool { return f.connected.Load() }

var httpc = &http.Client{Timeout: 10 * time.Second}

func getJSON(ctx context.Context, u string, v any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	res, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, res.Status)
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// Resolve fills Symbol from /api/v3/exchangeInfo and fails if a market is missing or its precisions disagree with deployments.
func Resolve(ctx context.Context, apiURL string, ms []streams.MarketInfo) error {
	var info struct {
		Symbols []struct {
			Symbol         string          `json:"symbol"`
			MarketAddress  string          `json:"marketAddress"`
			PricePrecision decimal.Decimal `json:"pricePrecision"`
			SizePrecision  decimal.Decimal `json:"sizePrecision"`
		} `json:"symbols"`
	}
	if err := getJSON(ctx, apiURL+"/api/v3/exchangeInfo", &info); err != nil {
		return err
	}
	for i := range ms {
		m := &ms[i]
		for _, s := range info.Symbols {
			if strings.EqualFold(s.MarketAddress, string(m.Market)) {
				if !s.PricePrecision.Equal(m.PricePrecision) || !s.SizePrecision.Equal(m.SizePrecision) {
					return fmt.Errorf("%s: kuru precisions %s/%s != deployments %s/%s", s.Symbol, s.PricePrecision, s.SizePrecision, m.PricePrecision, m.SizePrecision)
				}
				m.Symbol = s.Symbol
			}
		}
		if m.Symbol == "" {
			return fmt.Errorf("market %s not in kuru exchangeInfo", m.Market)
		}
	}
	return nil
}

// Run reconnects with exponential backoff until ctx ends.
func (f *Feed) Run(ctx context.Context) error {
	f.state = map[string]*mstate{}
	for _, m := range f.Markets {
		f.state[strings.ToLower(string(m.Market))] = &mstate{info: m, seq: uint64(time.Now().UnixMilli())}
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
		slog.Warn("kuru ws disconnected", "err", err, "retry", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
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
	f.seen = map[string]int{}

	var params []string
	for _, st := range f.state {
		s := strings.ToLower(st.info.Symbol)
		params = append(params, s+"@depth", s+"@trade")
	}
	sub, _ := json.Marshal(map[string]any{"method": "SUBSCRIBE", "params": params, "id": 1})
	if err := c.Write(ctx, websocket.MessageText, sub); err != nil {
		return err
	}
	// REST snapshot so books are fresh even if the socket is slow to send its first depthUpdate.
	for _, st := range f.state {
		if err := f.restBook(ctx, st); err != nil {
			slog.Warn("rest depth", "market", st.info.Symbol, "err", err)
		}
	}

	msgs, errc := make(chan []byte, 256), make(chan error, 1)
	go func() {
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				errc <- err
				return
			}
			select {
			case msgs <- b:
			case <-ctx.Done():
				return
			}
		}
	}()
	f.connected.Store(true)
	slog.Info("kuru ws connected", "markets", len(f.state))
	last := time.Now()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errc:
			return err
		case b := <-msgs:
			last = time.Now()
			f.handle(ctx, b)
		case <-tick.C:
			if time.Since(last) > idleMax {
				return errors.New("idle")
			}
			f.publishRefs(ctx)
			for _, st := range f.state {
				if len(st.bids)+len(st.asks) > 0 && time.Since(st.pubAt) > bookRefresh {
					f.publishBook(ctx, st)
				}
			}
		}
	}
}

type wsHead struct {
	E  string `json:"e"`
	Ts int64  `json:"E"`
	S  string `json:"s"`
}

type wsDepth struct {
	Bids [][2]string `json:"b"`
	Asks [][2]string `json:"a"`
}

type wsTrade struct {
	B uint64 `json:"b"` // block
	T string `json:"t"` // trade id, shared by fills of one taker order
	P string `json:"p"`
	Q string `json:"q"`
	M bool   `json:"m"` // buyer is maker, so the taker sold
	// exact-name sinks: without them encoding/json folds "T" into t and "M" into m
	XT json.RawMessage `json:"T"`
	XM json.RawMessage `json:"M"`
}

func (f *Feed) handle(ctx context.Context, b []byte) {
	var h wsHead
	if err := json.Unmarshal(b, &h); err != nil {
		slog.Warn("kuru ws message", "err", err, "msg", string(b[:min(len(b), 200)]))
		return
	}
	st := f.state[strings.ToLower(h.S)]
	if st == nil {
		return
	}
	switch h.E {
	case "depthUpdate": // carries the full book (observed live 2026-09-30), so it replaces ours
		var d wsDepth
		err := json.Unmarshal(b, &d)
		if err == nil {
			st.bids, st.asks, err = levels(st.info, d.Bids, d.Asks)
		}
		if err != nil {
			slog.Warn("depth", "market", st.info.Symbol, "err", err)
			return
		}
		f.publishBook(ctx, st)
	case "trade":
		t, key, err := decodeTrade(st.info, h, b)
		if err != nil {
			slog.Warn("trade", "err", err, "msg", string(b))
			return
		}
		if !f.fresh(ctx, key) {
			return
		}
		if err := streams.Publish(ctx, f.RDB, streams.Trades(string(st.info.Market)), "", t); err != nil {
			slog.Error("publish trade", "market", st.info.Market, "err", err)
		}
		st.lastTrade = t.Px.InexactFloat64()
		if f.Candles != nil {
			f.Candles.Add(ctx, t)
		}
	}
}

// fresh claims key for its nth occurrence this session, so identical fills in one session all count
// while a frame replayed after a reconnect (same n in the new session) is dropped.
// ponytail: occurrence map resets past 100k keys; an identical fill straddling a reset within dedupTTL would be dropped.
func (f *Feed) fresh(ctx context.Context, key string) bool {
	if f.seen == nil || len(f.seen) > 100_000 {
		f.seen = map[string]int{}
	}
	f.seen[key]++
	ok, err := f.RDB.SetNX(ctx, "trade:seen:"+key+"#"+strconv.Itoa(f.seen[key]), 1, dedupTTL).Result()
	if err != nil {
		slog.Warn("trade dedupe", "err", err)
		return true
	}
	return ok
}

// decodeTrade also returns the frame's identity: market, block, trade id, price, size, side.
func decodeTrade(m streams.MarketInfo, h wsHead, b []byte) (streams.Trade, string, error) {
	var w wsTrade
	if err := json.Unmarshal(b, &w); err != nil {
		return streams.Trade{}, "", err
	}
	px, err := decimal.NewFromString(w.P)
	if err != nil {
		return streams.Trade{}, "", err
	}
	sz, err := decimal.NewFromString(w.Q)
	if err != nil {
		return streams.Trade{}, "", err
	}
	side := "buy"
	if w.M {
		side = "sell"
	}
	ts := h.Ts
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	key := fmt.Sprintf("%s:%d:%s:%s:%s:%t", strings.ToLower(string(m.Market)), w.B, w.T, w.P, w.Q, w.M)
	return streams.Trade{Market: m.Market, Ts: ts, Px: px.Shift(-priceExp), Sz: sz.Div(m.SizePrecision), Side: side}, key, nil
}

// levels converts raw [price1e18, sizeRaw] pairs to top-20 display levels: bids high→low, asks low→high.
func levels(m streams.MarketInfo, rb, ra [][2]string) (bids, asks []streams.Level, err error) {
	conv := func(raw [][2]string, desc bool) ([]streams.Level, error) {
		out := make([]streams.Level, 0, len(raw))
		for _, l := range raw {
			px, err := decimal.NewFromString(l[0])
			if err != nil {
				return nil, err
			}
			sz, err := decimal.NewFromString(l[1])
			if err != nil {
				return nil, err
			}
			if sz.IsPositive() {
				out = append(out, streams.Level{px.Shift(-priceExp), sz.Div(m.SizePrecision)})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i][0].GreaterThan(out[j][0]) == desc })
		return out[:min(len(out), depth)], nil
	}
	if bids, err = conv(rb, true); err != nil {
		return
	}
	asks, err = conv(ra, false)
	return
}

func (f *Feed) restBook(ctx context.Context, st *mstate) error {
	var d struct {
		Bids [][2]string `json:"bids"`
		Asks [][2]string `json:"asks"`
	}
	if err := getJSON(ctx, f.APIURL+"/api/v3/depth?limit=100&symbol="+url.QueryEscape(st.info.Symbol), &d); err != nil {
		return err
	}
	bids, asks, err := levels(st.info, d.Bids, d.Asks)
	if err != nil {
		return err
	}
	st.bids, st.asks = bids, asks
	f.publishBook(ctx, st)
	return nil
}

func (f *Feed) publishBook(ctx context.Context, st *mstate) {
	st.pubAt = time.Now()
	st.seq++ // ponytail: seeded from unix ms so it stays monotonic across restarts at < 1000 publishes/s
	snap := streams.BookSnapshot{Market: st.info.Market, Seq: st.seq, Ts: time.Now().UnixMilli(), Bids: st.bids, Asks: st.asks}
	if err := streams.Publish(ctx, f.RDB, streams.Book(string(st.info.Market)), streams.BookLatest(string(st.info.Market)), snap); err != nil {
		slog.Error("publish book", "market", st.info.Market, "err", err)
	}
}

// mid is the display mid, 0 when either side is empty.
func (st *mstate) mid() float64 {
	if len(st.bids) == 0 || len(st.asks) == 0 {
		return 0
	}
	return st.bids[0][0].Add(st.asks[0][0]).InexactFloat64() / 2
}

func (f *Feed) publishRefs(ctx context.Context) {
	now := time.Now()
	for k, st := range f.state {
		mid := st.mid()
		x := mid
		if x == 0 {
			if st.lastTrade == 0 && st.ref.ref == 0 && !st.booted {
				st.booted = true
				var tr []struct {
					Price decimal.Decimal `json:"price"`
				}
				if err := getJSON(ctx, f.APIURL+"/api/v3/trades?limit=1&symbol="+url.QueryEscape(st.info.Symbol), &tr); err != nil || len(tr) == 0 {
					slog.Warn("ref bootstrap", "market", k, "err", err)
				} else {
					st.lastTrade = tr[0].Price.Shift(-priceExp).InexactFloat64()
				}
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
