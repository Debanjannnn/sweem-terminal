package hub

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

const book = "book.0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f"

type fake struct {
	starts atomic.Int32
	emit   chan func(any)
	done   chan string
}

func newHub(t *testing.T) (*Hub, *fake) {
	f := &fake{emit: make(chan func(any), 4), done: make(chan string, 4)}
	h := &Hub{ctx: t.Context(), topics: map[string]*topic{}, run: func(ctx context.Context, tp string, emit func(any)) error {
		f.starts.Add(1)
		f.emit <- emit
		<-ctx.Done()
		f.done <- tp
		return nil
	}}
	return h, f
}

func client(n int) (*Client, *atomic.Bool) {
	var dropped atomic.Bool
	return &Client{send: make(chan []byte, n), drop: func() { dropped.Store(true) }}, &dropped
}

func recv(t *testing.T, c *Client) string {
	select {
	case b := <-c.send:
		return string(b)
	case <-time.After(time.Second):
		t.Fatal("no frame")
		return ""
	}
}

func TestFanOutSnapshotAndStop(t *testing.T) {
	h, f := newHub(t)
	a, _ := client(8)
	b, _ := client(8)
	h.Sub(a, book)
	h.Sub(b, book)
	emit := <-f.emit
	emit(map[string]int{"seq": 1})
	want := `{"data":{"seq":1},"topic":"` + book + `"}`
	if recv(t, a) != want || recv(t, b) != want {
		t.Fatal("fan-out")
	}
	late, _ := client(8)
	h.Sub(late, book)
	if recv(t, late) != want {
		t.Fatal("late subscriber missed snapshot")
	}
	if f.starts.Load() != 1 {
		t.Fatalf("readers started: %d", f.starts.Load())
	}
	h.Unsub(a, book)
	h.Unsub(b, book)
	h.Unsub(late, book)
	if got := <-f.done; got != book {
		t.Fatal(got)
	}
	emit(map[string]int{"seq": 2}) // stale reader after stop must not deliver
	if len(a.send) != 0 {
		t.Fatal("delivered after unsub")
	}
}

func TestTradesNotReplayed(t *testing.T) {
	h, f := newHub(t)
	tr := "trades.0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f"
	a, _ := client(8)
	h.Sub(a, tr)
	(<-f.emit)(1)
	recv(t, a)
	late, _ := client(8)
	h.Sub(late, tr)
	if len(late.send) != 0 {
		t.Fatal("trade replayed")
	}
}

func TestSlowClientDropped(t *testing.T) {
	h, f := newHub(t)
	slow, dropped := client(1)
	h.Sub(slow, book)
	emit := <-f.emit
	emit(1)
	emit(2)
	if !dropped.Load() {
		t.Fatal("slow client kept")
	}
}

func TestValidTopic(t *testing.T) {
	for tp, ok := range map[string]bool{
		book: true,
		"nav.0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f":        false,
		"candles.0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f.1m": true,
		"candles.0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f.1d": true,
		"candles.0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f.2m": false,
		"candles.0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f":    false,
		"rates":  true,
		"rates.": false,
		"trades.0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f": true,
		"book.0x5BDEA6F9F9abA34F4EcB9B865646A792b835ef7f":   false,
		"ref.0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f":    false,
		"book.0x5bde": false,
		"book.":       false,
		book + " ":    false,
	} {
		if ValidTopic(tp) != ok {
			t.Errorf("%q", tp)
		}
	}
}
