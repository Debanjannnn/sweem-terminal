package kuru

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/book"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/candle"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/store"
	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/ws"
)

// same MON/USDC market the live fixtures came from
const (
	fxDeltaMon  = "00000000000000004b584d4401020100000000000000011dfdbe356828c8f5a5d5ed4f69dde0816f4058ef610000000000005f7f0000000000005f7e00000000004c67910000000003db3ece1111111111111111111111111111111111111111111111111111111111111111000000010200000000000050140000000000000000000000003b9aca000000000000000000000000003b9aca000000000000000000000000000000000000000001"
	fxTradesMon = "00000000000000004b584d4401030300000000000000011dfdbe356828c8f5a5d5ed4f69dde0816f4058ef610000000000005f820000000000005f8100000000004c67920000000003db3ecf11111111111111111111111111111111111111111111111111111111111111110000000100000000000010920000010000000000004e2000000000000000000000000002faf080000000006ab2504b"
)

var testMarket = store.Market{
	ID: "0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61", Venue: store.VenueKuru, Symbol: "MONUSDC",
	Address: "0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61",
	// pricePrecision 1e6, sizePrecision 1e8, base 18, quote 6
	PriceDecimals: 6, SizeDecimals: 8, BaseDecimals: 18, QuoteDecimals: 6,
}

func frameFor(t *testing.T, fx string, subID uint64) []byte {
	t.Helper()
	raw, err := hex.DecodeString(fx)
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint64(raw[:8], subID)
	return raw
}

// fakeGateway is the real codec over a real socket: the script talks to the
// client exactly as the gateway would.
func fakeGateway(t *testing.T, script func(*testing.T, *ws.Conn)) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/depth", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("symbol") != "MONUSDC" || r.URL.Query().Get("state") != "finalized" {
			t.Errorf("depth query = %q", r.URL.RawQuery)
		}
		w.Write([]byte(`{"feedEpoch":285,"data":{"bids":[{"price":"19000","total_base":"1000000"}],"asks":[]}}`))
	})
	done := make(chan struct{})
	var once sync.Once
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.Accept(w, r)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		defer once.Do(func() { close(done) })
		script(t, conn)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close(); <-done })

	c := New(10143)
	c.gateway = srv.URL
	c.gatewayWS = "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	c.markets = map[string]store.Market{testMarket.ID: testMarket}
	c.order = []string{testMarket.ID}
	return c
}

func readSub(t *testing.T, conn *ws.Conn) subReq {
	t.Helper()
	conn.NetConn().SetReadDeadline(time.Now().Add(5 * time.Second))
	op, msg, err := conn.Read()
	if err != nil {
		t.Fatalf("read sub: %v", err)
	}
	if op != ws.OpText {
		t.Fatalf("opcode %d is not a control frame", op)
	}
	var r subReq
	if err := json.Unmarshal(msg, &r); err != nil {
		t.Fatalf("sub %q is not JSON: %v", msg, err)
	}
	return r
}

func ack(t *testing.T, conn *ws.Conn, r subReq) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"op": "ack", "feedEpoch": 285, "id": r.ID, "t": r.T})
	if err := conn.WriteText(b); err != nil {
		t.Fatal(err)
	}
}

// A proposed book can be dropped out from under us, so branchDropped must
// discard the derived book and bootstrap again rather than keep serving it.
func TestBranchDroppedRollsBack(t *testing.T) {
	type resub struct {
		unsubbed uint64
		req      subReq
	}
	resubs := make(chan resub, 1)

	c := fakeGateway(t, func(t *testing.T, conn *ws.Conn) {
		bookSub := readSub(t, conn)
		if bookSub.T != "l2Delta@p" {
			t.Errorf("book topic = %q, want the proposed tier", bookSub.T)
		}
		if bookSub.Seq != 0 {
			t.Errorf("a fresh l2Delta must omit seq so the gateway snapshots, got %d", bookSub.Seq)
		}
		tradeSub := readSub(t, conn)
		if tradeSub.T != "trades@f" {
			t.Errorf("trades topic = %q, want the finalized tier", tradeSub.T)
		}
		ack(t, conn, bookSub)
		ack(t, conn, tradeSub)

		conn.WriteBinary(frameFor(t, fxL2Book, bookSub.ID))
		conn.WriteBinary(frameFor(t, fxDeltaMon, bookSub.ID))
		conn.WriteBinary(frameFor(t, fxTradesMon, tradeSub.ID))
		conn.WriteBinary(frameFor(t, fxLifecycleDrop, bookSub.ID))

		un := readSub(t, conn)
		again := readSub(t, conn)
		resubs <- resub{unsubbed: un.ID, req: again}
		if un.Op != "unsub" {
			t.Errorf("op after a dropped branch = %q, want unsub", un.Op)
		}
		ack(t, conn, again)
		conn.WriteBinary(frameFor(t, fxL2Book, again.ID))
		<-t.Context().Done()
	})

	books := make(chan book.Book, 16)
	ticks := make(chan candle.Tick, 16)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	go c.Stream(ctx, Handlers{
		Book: func(b book.Book) { books <- b },
		Tick: func(tk candle.Tick) { ticks <- tk },
	})

	seed := recv(t, books) // REST depth warms the market before the socket answers
	eq(t, "seed bids", len(seed.Bids), 1)
	eq(t, "seed price", seed.Bids[0].Price, "19000000000000000") // 19000 / 1e6 in WAD

	snap := recv(t, books)
	eq(t, "snapshot bids", len(snap.Bids), 1)
	eq(t, "snapshot price", snap.Bids[0].Price, "20000000000000000")
	eq(t, "snapshot size", snap.Bids[0].Size, "500000000000000000000")

	withAsk := recv(t, books)
	eq(t, "ask after delta", len(withAsk.Asks), 1)
	eq(t, "ask price", withAsk.Asks[0].Price, "20500000000000000")

	tk := recv(t, ticks)
	eq(t, "tick market", tk.MarketID, testMarket.ID)
	eq(t, "tick side", tk.Side, "buy")
	eq(t, "tick price", tk.Price, "20000000000000000")

	r := recv(t, resubs)
	eq(t, "unsubbed the dropped branch", r.unsubbed, uint64(1))
	eq(t, "rebootstrapped the same topic", r.req.T, "l2Delta@p")
	eq(t, "rebootstrap must not replay a dropped cursor", r.req.Seq, uint64(0))
	if r.req.ID == r.unsubbed {
		t.Fatal("the replacement subscription reused the dropped id")
	}

	after := recv(t, books)
	eq(t, "the rolled-back book has no ask", len(after.Asks), 0)
	eq(t, "the rolled-back book is the fresh snapshot", len(after.Bids), 1)
}

func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the next frame")
		panic("unreachable")
	}
}

// Streams are sparse: the predecessor is whatever the publisher last sent, not
// seq-1, so a gap can only be detected by that explicit link.
func TestSubAdvance(t *testing.T) {
	tests := []struct {
		name          string
		cursor        uint64
		bridged       bool
		seq, prev     uint64
		want          bool
		wantAfterSeq  uint64
		wantAfterFlag bool
	}{
		{"live join with no cursor", 0, false, 900, 880, true, 900, true},
		{"replay bridge lands past the cursor", 100, false, 140, 90, true, 140, true},
		{"replay bridge with a null predecessor", 100, false, 140, 0, true, 140, true},
		{"replay bridge at or before the cursor", 100, false, 100, 0, false, 100, false},
		{"replay bridge skips past the cursor", 100, false, 140, 120, false, 100, false},
		{"sparse successor links exactly", 140, true, 900, 140, true, 900, true},
		{"successor off by one is still a gap", 140, true, 900, 141, false, 140, true},
		{"repeated frame", 140, true, 140, 139, false, 140, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &gwSub{seq: tt.cursor, bridged: tt.bridged}
			eq(t, "advance", s.advance(tt.seq, tt.prev), tt.want)
			eq(t, "cursor", s.seq, tt.wantAfterSeq)
			eq(t, "bridged", s.bridged, tt.wantAfterFlag)
		})
	}
}

// A market with no book changes still has a live book. It must be re-published
// while the socket is healthy, or its cache entry expires and reads as cold.
func TestQuietBookIsRefreshed(t *testing.T) {
	prev := bookRefreshEvery
	bookRefreshEvery = 50 * time.Millisecond
	t.Cleanup(func() { bookRefreshEvery = prev })

	c := fakeGateway(t, func(t *testing.T, conn *ws.Conn) {
		bookSub := readSub(t, conn)
		tradeSub := readSub(t, conn)
		ack(t, conn, bookSub)
		ack(t, conn, tradeSub)
		conn.WriteBinary(frameFor(t, fxL2Book, bookSub.ID))
		// nothing trades; only keepalive pongs arrive
		for range 3 {
			time.Sleep(80 * time.Millisecond)
			conn.WriteText([]byte(`{"op":"pong"}`))
		}
		<-t.Context().Done()
	})

	books := make(chan book.Book, 16)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	go c.Stream(ctx, Handlers{Book: func(b book.Book) { books <- b }, Tick: func(candle.Tick) {}})

	recv(t, books) // REST seed
	snap := recv(t, books)
	again := recv(t, books)
	eq(t, "refresh re-publishes the same book", again.Bids[0].Price, snap.Bids[0].Price)
	if again.Ts < snap.Ts {
		t.Fatalf("refreshed book is older than the snapshot: %d < %d", again.Ts, snap.Ts)
	}
}
