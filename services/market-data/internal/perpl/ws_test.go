package perpl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/book"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/candle"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/ws"
)

// A scripted Perpl market-data server: it speaks the real frame shapes over the
// real codec, so the client's decoding is tested across the wire rather than
// against itself.
func perplWS(t *testing.T, conns *atomic.Int32, subs chan<- []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != MarketDataPath {
			http.NotFound(w, r)
			return
		}
		n := conns.Add(1)
		conn, err := ws.Accept(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		_, msg, err := conn.Read()
		if err != nil {
			return
		}
		var req struct {
			MT   int `json:"mt"`
			Subs []struct {
				Stream    string `json:"stream"`
				Subscribe bool   `json:"subscribe"`
			} `json:"subs"`
		}
		if err := json.Unmarshal(msg, &req); err != nil || req.MT != 5 {
			return
		}
		sid := map[string]int{}
		entries := []string{}
		resp := []map[string]any{}
		for i, s := range req.Subs {
			sid[s.Stream] = i + 1
			entries = append(entries, s.Stream)
			resp = append(resp, map[string]any{"stream": s.Stream, "sid": i + 1, "status": map[string]any{"code": 0}})
		}
		select {
		case subs <- entries:
		default:
		}
		send := func(v any) {
			b, _ := json.Marshal(v)
			conn.WriteText(b)
		}
		send(map[string]any{"mt": 6, "subs": resp})

		bookSID, tradeSID, candleSID, hbSID := sid["order-book@16"], sid["trades@16"], sid["candles@16*60"], sid["heartbeat@10143"]
		send(map[string]any{"mt": 15, "sid": bookSID, "sn": 1, "at": map[string]any{"t": 1_700_000_000_000},
			"bid": []map[string]any{{"p": 817700, "s": 12345, "o": 1}, {"p": 817600, "s": 20000, "o": 2}},
			"ask": []map[string]any{{"p": 817800, "s": 10000, "o": 1}}})
		send(map[string]any{"mt": 16, "sid": bookSID, "sn": 2, "at": map[string]any{"t": 1_700_000_001_000},
			"bid": []map[string]any{{"p": 817600, "s": 20000, "o": 0}}})
		send(map[string]any{"mt": 17, "sid": tradeSID, "sn": 1,
			"d": []map[string]any{{"at": map[string]any{"t": 1_700_000_002_000}, "p": 817750, "s": 5000, "sd": 2}}})
		send(map[string]any{"mt": 11, "sid": candleSID, "sn": 1, "r": 60,
			"d": []map[string]any{{"t": 1_700_000_040_000, "o": 817000, "c": 817500, "h": 818000, "l": 816000, "v": "2500000", "n": 3}}})
		send(map[string]any{"mt": 100, "sid": hbSID, "sn": 1, "h": 64_000_000})
		if n == 1 {
			// a heartbeat that skips 2,3,4 means frames were lost
			send(map[string]any{"mt": 100, "sid": hbSID, "sn": 5, "h": 64_000_004})
		}
		for {
			if _, _, err := conn.Read(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PERPL_WS_URL", "ws"+strings.TrimPrefix(srv.URL, "http"))
	return srv
}

func TestStream(t *testing.T) {
	apiServer(t, map[string]string{"/v1/pub/context": contextPayload})
	t.Setenv("PERPL_MARKET_SYMBOLS", "BTC")
	var conns atomic.Int32
	subs := make(chan []string, 4)
	perplWS(t, &conns, subs)

	books := make(chan book.Book, 16)
	ticks := make(chan candle.Tick, 16)
	candles := make(chan candle.Candle, 16)
	c := New()
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Stream(ctx, Handlers{
		Book:   func(b book.Book) { books <- b },
		Tick:   func(tk candle.Tick) { ticks <- tk },
		Candle: func(cd candle.Candle) { candles <- cd },
	})

	t.Run("one batched frame, inside the 16 subscription cap", func(t *testing.T) {
		got := recv(t, subs)
		if len(got) > maxSubs {
			t.Fatalf("%d subscriptions, cap is %d", len(got), maxSubs)
		}
		want := []string{"heartbeat@10143", "order-book@16", "trades@16", "candles@16*60"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("subs = %v, want %v", got, want)
		}
	})

	t.Run("book snapshot is scaled by the market decimals", func(t *testing.T) {
		b := recv(t, books)
		// p 817700 at price_decimals 1, s 12345 at size_decimals 5
		if len(b.Bids) != 2 || b.Bids[0].Price != "81770000000000000000000" || b.Bids[0].Size != "123450000000000000" {
			t.Fatalf("bids = %v", b.Bids)
		}
		if len(b.Asks) != 1 || b.Asks[0].Price != "81780000000000000000000" {
			t.Fatalf("asks = %v", b.Asks)
		}
		if b.MarketID != "16" || b.Venue != "perpl" {
			t.Fatalf("identity = %s/%s", b.MarketID, b.Venue)
		}
	})

	t.Run("a level with zero orders is removed", func(t *testing.T) {
		b := recv(t, books)
		if len(b.Bids) != 1 || b.Bids[0].Price != "81770000000000000000000" {
			t.Fatalf("bids after the delta = %v", b.Bids)
		}
	})

	t.Run("trades become ticks", func(t *testing.T) {
		tk := recv(t, ticks)
		if tk.Price != "81775000000000000000000" || tk.Size != "50000000000000000" {
			t.Errorf("tick = %+v", tk)
		}
		if tk.Side != "sell" || tk.Ts != 1_700_000_002_000 {
			t.Errorf("tick side/ts = %s/%d", tk.Side, tk.Ts)
		}
	})

	t.Run("candles arrive at the subscribed resolution", func(t *testing.T) {
		cd := recv(t, candles)
		if cd.Resolution != 60 || cd.OpenTime != 1_700_000_040 || cd.Close != "81750000000000000000000" {
			t.Errorf("candle = %+v", cd)
		}
	})

	t.Run("a heartbeat sequence gap forces a reconnect", func(t *testing.T) {
		deadline := time.Now().Add(10 * time.Second)
		for conns.Load() < 2 {
			if time.Now().After(deadline) {
				t.Fatalf("still on connection %d: a lost-frame gap must resync the session", conns.Load())
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}

func TestParseStream(t *testing.T) {
	cases := []struct {
		name string
		want subInfo
	}{
		{"order-book@16", subInfo{kind: "order-book", marketID: "16"}},
		{"trades@64", subInfo{kind: "trades", marketID: "64"}},
		{"candles@32*3600", subInfo{kind: "candles", marketID: "32", res: 3600}},
		{"heartbeat@10143", subInfo{kind: "heartbeat", marketID: "10143"}},
	}
	for _, tc := range cases {
		got := parseStream(tc.name)
		if got != tc.want {
			t.Errorf("parseStream(%q) = %+v, want %+v", tc.name, got, tc.want)
		}
		if round := streamName(got); round != tc.name {
			t.Errorf("streamName round trip = %q, want %q", round, tc.name)
		}
	}
}

func TestRequestBudget(t *testing.T) {
	s := &session{conn: nil}
	for i := range maxReqs {
		s.reqs = append(s.reqs, time.Now())
		_ = i
	}
	if err := s.request([]string{"trades@16"}, nil); err == nil {
		t.Fatal("want the 10/min budget to refuse rather than earn a 1008 close")
	}
}

func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		var zero T
		t.Fatal(fmt.Sprintf("timed out waiting for %T", zero))
		return zero
	}
}

// Replays the frame interleaving recorded live from Perpl testnet, where `sn`
// is the Monad block number: snapshots arrive stamped with blocks far behind
// head, three streams share one sn when they changed in the same block, and a
// single stream jumps several blocks when nothing happened in between. None of
// that is a gap. The old per-stream "+1" check fired on every one of these and
// spent the 10/min request budget until the socket was dropped with 1008.
func TestStreamTreatsSequenceAsBlockNumber(t *testing.T) {
	apiServer(t, map[string]string{"/v1/pub/context": contextPayload})
	t.Setenv("PERPL_MARKET_SYMBOLS", "BTC")

	var conns, clientFrames atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conns.Add(1)
		conn, err := ws.Accept(w, r)
		if err != nil {
			return
		}
		defer conn.Close()
		_, msg, err := conn.Read()
		if err != nil {
			return
		}
		clientFrames.Add(1)
		var req struct {
			Subs []struct {
				Stream string `json:"stream"`
			} `json:"subs"`
		}
		json.Unmarshal(msg, &req)
		// live sid shape: heartbeat 5000, order-book 10000NN, trades 20000NN, candles 3000NN01
		live := map[string]int{"heartbeat@10143": 5000, "order-book@16": 1000016, "trades@16": 2000016, "candles@16*60": 300001601}
		resp := []map[string]any{}
		for _, s := range req.Subs {
			resp = append(resp, map[string]any{"stream": s.Stream, "sid": live[s.Stream], "status": map[string]any{"code": 0}})
		}
		send := func(v any) {
			b, _ := json.Marshal(v)
			conn.WriteText(b)
		}
		send(map[string]any{"mt": 6, "subs": resp})

		trade := []map[string]any{{"at": map[string]any{"t": 1_700_000_002_000}, "p": 817750, "s": 5000, "sd": 2}}
		bar := []map[string]any{{"t": 1_700_000_040_000, "o": 817000, "c": 817500, "h": 818000, "l": 816000, "v": "2500000", "n": 3}}
		lvl := []map[string]any{{"p": 817700, "s": 12345, "o": 1}}

		// snapshots are stamped with the block each stream last changed at,
		// which is well behind the current head
		send(map[string]any{"mt": 15, "sid": 1000016, "sn": 64324913, "bid": lvl, "ask": lvl})
		send(map[string]any{"mt": 17, "sid": 2000016, "sn": 64324910, "d": trade})
		send(map[string]any{"mt": 11, "sid": 300001601, "sn": 64324910, "r": 60, "d": bar})
		send(map[string]any{"mt": 100, "sid": 5000, "sn": 64324942, "h": 64324942})
		// one block, three streams, one shared sn
		send(map[string]any{"mt": 100, "sid": 5000, "sn": 64324943, "h": 64324943})
		send(map[string]any{"mt": 16, "sid": 1000016, "sn": 64324943, "bid": lvl})
		send(map[string]any{"mt": 18, "sid": 2000016, "sn": 64324943, "d": trade})
		send(map[string]any{"mt": 12, "sid": 300001601, "sn": 64324943, "r": 60, "d": bar})
		// quiet blocks: the book stream skips ahead, the heartbeat does not
		for sn := 64324944; sn <= 64324950; sn++ {
			send(map[string]any{"mt": 100, "sid": 5000, "sn": sn, "h": sn})
		}
		send(map[string]any{"mt": 16, "sid": 1000016, "sn": 64324945, "bid": lvl})
		send(map[string]any{"mt": 16, "sid": 1000016, "sn": 64324950, "bid": lvl})

		for {
			if _, _, err := conn.Read(); err != nil {
				return
			}
			clientFrames.Add(1) // any frame past the first subscribe is a resync
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PERPL_WS_URL", "ws"+strings.TrimPrefix(srv.URL, "http"))

	books := make(chan book.Book, 32)
	c := New()
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Stream(ctx, Handlers{Book: func(b book.Book) { books <- b }})

	for range 4 { // snapshot plus three updates, none of them filtered out
		recv(t, books)
	}
	time.Sleep(300 * time.Millisecond) // let any resync the client wanted to send land

	if got := clientFrames.Load(); got != 1 {
		t.Errorf("client sent %d frames, want exactly 1: block-number sn must never trigger a resubscribe", got)
	}
	if got := conns.Load(); got != 1 {
		t.Errorf("opened %d connections, want 1: none of these frames is a real gap", got)
	}
}

func TestHeartbeatGapDetection(t *testing.T) {
	hb := func(sn int64) wireFrame { return wireFrame{MT: mtHeartbeat, SID: 5000, SN: &sn} }
	data := func(sn int64) wireFrame { return wireFrame{MT: mtBookUpd, SID: 1000016, SN: &sn} }

	cases := []struct {
		name    string
		frames  []wireFrame
		wantErr bool
	}{
		{"contiguous blocks are healthy", []wireFrame{hb(100), hb(101), hb(102)}, false},
		{"first heartbeat sets the baseline", []wireFrame{hb(64324942)}, false},
		{"a skipped block means frames were lost", []wireFrame{hb(100), hb(105)}, true},
		{"a repeated block is a reorg, not a loss", []wireFrame{hb(100), hb(100)}, false},
		{"a rewound block is a reorg, not a loss", []wireFrame{hb(100), hb(98)}, false},
		{"data streams never trigger a gap however far they jump",
			[]wireFrame{hb(100), data(60), data(100), data(140), hb(101)}, false},
		{"a frame with no sn is ignored", []wireFrame{hb(100), {MT: mtBookUpd, SID: 1000016}, hb(101)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &session{hbSID: 5000, info: map[int]subInfo{}, books: map[string]*book.Book{}}
			var err error
			for _, f := range tc.frames {
				if e := s.checkHeartbeat(f); e != nil {
					err = e
				}
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
