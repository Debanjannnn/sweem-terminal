package perpl

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/config"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/book"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/candle"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/ws"
)

const (
	MarketDataPath = "/ws/v1/market-data"

	// server limits, same on both networks
	maxSubs    = 16
	maxReqs    = 10
	reqWindow  = time.Minute
	streamsPer = 3 // order-book, trades, candles per market

	// the server keeps this socket alive itself. an application ping would
	// spend 20% of a 10/min budget to buy nothing.
	readTimeout = 90 * time.Second

	// only a session that lasted this long resets the backoff, so repeated
	// heartbeat gaps escalate instead of pinning the retry at one second
	healthySession = 5 * time.Minute
)

// message types we act on
const (
	mtSubscribe   = 5
	mtSubResponse = 6
	mtCandlesSnap = 11
	mtCandlesUpd  = 12
	mtBookSnap    = 15
	mtBookUpd     = 16
	mtTradesSnap  = 17
	mtTradesUpd   = 18
	mtHeartbeat   = 100
)

var backoffs = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, time.Minute}

type Handlers struct {
	Book   func(book.Book)
	Tick   func(candle.Tick)
	Candle func(candle.Candle)
}

// Stream owns the public market-data socket and runs until ctx is cancelled.
// It never opens /ws/v1/trading: that socket belongs to the executor and shares
// a per-wallet connection cap with it.
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
		code := ws.CloseCode(err)
		if code == 1001 { // server restart: it is coming straight back
			slog.Info("perpl market-data socket restarting", "code", code)
			continue
		}
		delay := backoffs[min(attempt, len(backoffs)-1)]
		attempt++
		slog.Warn("perpl market-data socket dropped", "code", code, "err", err, "retry_in", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return ctx.Err()
}

type subInfo struct {
	kind     string // order-book, trades, candles, heartbeat
	marketID string
	res      int
}

type session struct {
	conn    *ws.Conn
	pc      *Context
	info    map[int]subInfo
	books   map[string]*book.Book
	reqs    []time.Time
	hbSID   int
	hbSN    int64
	hbSeen  bool
	handler Handlers
}

func (c *Client) session(ctx context.Context, h Handlers) error {
	pc := c.Cached()
	if pc == nil {
		var err error
		if pc, err = c.Refresh(ctx); err != nil {
			return err
		}
	}
	conn, err := ws.Dial(c.wsURL+MarketDataPath, 15*time.Second)
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

	s := &session{
		conn: conn, pc: pc, handler: h, hbSID: -1,
		info: map[int]subInfo{}, books: map[string]*book.Book{},
	}
	if err := s.request(streamNames(pc), nil); err != nil {
		return err
	}
	// Handlers run inline on this goroutine, so a slow handler delays the pong
	// the server pings for every 5s and earns a 1008/1013 close. Measured live:
	// ~3.5 frames/s, no handler over 200ms, so the headroom is large.
	// ponytail: inline handling, move to a buffered read queue if a handler ever
	// blocks (redis or pg on this path) or the stream grows past a few markets
	for {
		conn.NetConn().SetReadDeadline(time.Now().Add(readTimeout))
		op, msg, err := conn.Read()
		if err != nil {
			return err
		}
		if op != ws.OpText {
			continue
		}
		if err := s.handle(msg); err != nil {
			return err
		}
	}
}

// streamNames batches every stream into one frame and stays inside the 16
// subscription cap.
func streamNames(pc *Context) []string {
	ids := pc.IDs()
	if want := config.GetEnvList("PERPL_MARKET_SYMBOLS", nil); len(want) > 0 {
		ids = filterBySymbol(pc, want)
	}
	out := []string{fmt.Sprintf("heartbeat@%d", pc.ChainID)}
	for _, id := range ids {
		if len(out)+streamsPer > maxSubs {
			break
		}
		out = append(out,
			fmt.Sprintf("order-book@%d", id),
			fmt.Sprintf("trades@%d", id),
			fmt.Sprintf("candles@%d*60", id))
	}
	return out
}

func filterBySymbol(pc *Context, want []string) []int {
	keep := map[string]bool{}
	for _, s := range want {
		keep[strings.ToUpper(strings.TrimSpace(s))] = true
	}
	out := []int{}
	for _, m := range pc.Markets {
		if keep[strings.ToUpper(m.Symbol)] {
			if id, err := strconv.Atoi(m.ID); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

type subEntry struct {
	Stream    string `json:"stream"`
	Subscribe bool   `json:"subscribe"`
}

// request sends one mt:5 frame, respecting the 10 requests/minute budget that
// closes the socket with 1008 when exceeded.
func (s *session) request(subscribe, unsubscribe []string) error {
	now := time.Now()
	kept := s.reqs[:0]
	for _, t := range s.reqs {
		if now.Sub(t) < reqWindow {
			kept = append(kept, t)
		}
	}
	s.reqs = kept
	if len(s.reqs) >= maxReqs {
		return fmt.Errorf("perpl market-data request budget (%d/min) is spent; reconnecting is cheaper than a 1008", maxReqs)
	}
	s.reqs = append(s.reqs, now)

	subs := make([]subEntry, 0, len(subscribe)+len(unsubscribe))
	for _, st := range unsubscribe {
		subs = append(subs, subEntry{Stream: st, Subscribe: false})
	}
	for _, st := range subscribe {
		subs = append(subs, subEntry{Stream: st, Subscribe: true})
	}
	b, err := json.Marshal(map[string]any{"mt": mtSubscribe, "subs": subs})
	if err != nil {
		return err
	}
	return s.conn.WriteText(b)
}

type wireLevel struct {
	P int64 `json:"p"`
	S int64 `json:"s"`
	O int   `json:"o"`
}

type wireTrade struct {
	At struct {
		T int64 `json:"t"`
	} `json:"at"`
	P  int64 `json:"p"`
	S  int64 `json:"s"`
	Sd int   `json:"sd"`
}

type wireFrame struct {
	MT  int    `json:"mt"`
	SID int    `json:"sid"`
	SN  *int64 `json:"sn"`
	R   int    `json:"r"`
	At  struct {
		T int64 `json:"t"`
	} `json:"at"`
	Bid  []wireLevel `json:"bid"`
	Ask  []wireLevel `json:"ask"`
	Subs []struct {
		Stream string `json:"stream"`
		SID    int    `json:"sid"`
		Status *struct {
			Code  int    `json:"code"`
			Error string `json:"error"`
		} `json:"status"`
	} `json:"subs"`
	D json.RawMessage `json:"d"`
}

func (s *session) handle(msg []byte) error {
	var f wireFrame
	if err := json.Unmarshal(msg, &f); err != nil {
		return fmt.Errorf("perpl sent a frame that is not JSON: %w", err)
	}
	if f.MT == mtSubResponse {
		for _, sub := range f.Subs {
			if sub.Status != nil && sub.Status.Code != 0 {
				// per-subscription failure; the socket and the rest stay up
				slog.Warn("perpl rejected a subscription", "stream", sub.Stream, "code", sub.Status.Code, "err", sub.Status.Error)
				continue
			}
			s.info[sub.SID] = parseStream(sub.Stream)
			if strings.HasPrefix(sub.Stream, "heartbeat@") {
				s.hbSID = sub.SID
			}
		}
		return nil
	}
	if err := s.checkHeartbeat(f); err != nil {
		return err
	}
	info := s.info[f.SID]
	switch f.MT {
	case mtBookSnap, mtBookUpd:
		s.onBook(f, info)
	case mtTradesSnap, mtTradesUpd:
		s.onTrades(f, info)
	case mtCandlesSnap, mtCandlesUpd:
		s.onCandles(f, info)
	case mtHeartbeat:
	}
	return nil
}

// `sn` is the Monad block number the frame reflects, not a per-message counter.
// Verified live: every heartbeat carries sn == h (the head block), and frames on
// different streams share one sn when they were produced in the same block
// (order-book@16, trades@16 and candles@16*60 all arrive at sn=64324943).
//
// So only the heartbeat is a loss detector: there is exactly one per block, so a
// forward skip means frames were dropped and the book may be desynced. A data
// stream only emits when it changed, so its sn legitimately jumps by however
// many blocks were quiet -- that is not a gap and must never trigger anything.
// Treating it as one is what spent the 10/min budget and killed the socket.
func (s *session) checkHeartbeat(f wireFrame) error {
	if f.SN == nil || (f.MT != mtHeartbeat && f.SID != s.hbSID) {
		return nil
	}
	seen, prev := s.hbSeen, s.hbSN
	s.hbSeen, s.hbSN = true, *f.SN
	// a repeated or rewound block is a reorg, not a loss: proposed blocks change
	if !seen || *f.SN <= prev || *f.SN == prev+1 {
		return nil
	}
	return fmt.Errorf("perpl heartbeat skipped blocks %d..%d, so frames were lost", prev+1, *f.SN-1)
}

func (s *session) onBook(f wireFrame, info subInfo) {
	m, ok := s.marketOf(info)
	if !ok {
		return
	}
	b := s.books[info.marketID]
	if b == nil || f.MT == mtBookSnap {
		b = &book.Book{MarketID: m.ID, Venue: m.Venue}
		s.books[info.marketID] = b
	}
	b.Apply(levels(f.Bid, m), levels(f.Ask, m))
	b.Ts = orNow(f.At.T)
	if s.handler.Book != nil {
		copy := *b
		s.handler.Book(copy)
	}
}

func (s *session) onTrades(f wireFrame, info subInfo) {
	m, ok := s.marketOf(info)
	if !ok || s.handler.Tick == nil {
		return
	}
	var trades []wireTrade
	if err := json.Unmarshal(f.D, &trades); err != nil {
		return
	}
	for _, t := range trades {
		side := "buy"
		if t.Sd == 2 {
			side = "sell"
		}
		s.handler.Tick(candle.Tick{
			MarketID: m.ID, Venue: m.Venue, Side: side,
			Price: scalePrice(t.P, m.PriceDecimals),
			Size:  scaleSize(t.S, m.SizeDecimals),
			Ts:    orNow(t.At.T),
		})
	}
}

func (s *session) onCandles(f wireFrame, info subInfo) {
	m, ok := s.marketOf(info)
	if !ok || s.handler.Candle == nil {
		return
	}
	var cs []wireCandle
	if err := json.Unmarshal(f.D, &cs); err != nil {
		return
	}
	res := f.R
	if res == 0 {
		res = info.res
	}
	for _, w := range cs {
		s.handler.Candle(candle.Candle{
			MarketID: m.ID, Resolution: res, OpenTime: w.T / 1000,
			Open:  scalePrice(w.O, m.PriceDecimals),
			High:  scalePrice(w.H, m.PriceDecimals),
			Low:   scalePrice(w.L, m.PriceDecimals),
			Close: scalePrice(w.C, m.PriceDecimals),
			// collateral Amount, not a base size
			Volume: scaleAmount(w.V, m.QuoteDecimals),
		})
	}
}

func (s *session) marketOf(info subInfo) (m marketView, ok bool) {
	id, err := strconv.Atoi(info.marketID)
	if err != nil {
		return m, false
	}
	sm, ok := s.pc.Market(id)
	if !ok {
		return m, false
	}
	return marketView{
		ID: sm.ID, Venue: sm.Venue,
		PriceDecimals: sm.PriceDecimals, SizeDecimals: sm.SizeDecimals, QuoteDecimals: sm.QuoteDecimals,
	}, true
}

type marketView struct {
	ID, Venue                                  string
	PriceDecimals, SizeDecimals, QuoteDecimals int
}

// a level with zero orders is a removal, which the book reads as size zero
func levels(ls []wireLevel, m marketView) []book.Level {
	out := make([]book.Level, 0, len(ls))
	for _, l := range ls {
		size := scaleSize(l.S, m.SizeDecimals)
		if l.O == 0 {
			size = "0"
		}
		out = append(out, book.Level{Price: scalePrice(l.P, m.PriceDecimals), Size: size})
	}
	return out
}

func parseStream(name string) subInfo {
	kind, rest, _ := strings.Cut(name, "@")
	id, resRaw, hasRes := strings.Cut(rest, "*")
	info := subInfo{kind: kind, marketID: id}
	if hasRes {
		info.res, _ = strconv.Atoi(resRaw)
	}
	return info
}

func streamName(info subInfo) string {
	if info.res > 0 {
		return fmt.Sprintf("%s@%s*%d", info.kind, info.marketID, info.res)
	}
	return info.kind + "@" + info.marketID
}

func orNow(ms int64) int64 {
	if ms > 0 {
		return ms
	}
	return time.Now().UnixMilli()
}
