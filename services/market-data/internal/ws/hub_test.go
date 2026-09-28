package ws_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/services/market-data/internal/ws"
)

// dial returns a real client connection to a real hub. Every assertion below
// reads bytes that crossed the hub, the server codec, a socket and the client
// codec, so a change to either side fails here instead of in the browser.
func dial(t *testing.T) (*ws.Hub, *ws.Conn) {
	t.Helper()
	hub := ws.NewHub()
	srv := httptest.NewServer(hub)
	t.Cleanup(srv.Close)
	c, err := ws.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return hub, c
}

func sendUp(t *testing.T, c *ws.Conn, op string, topics ...string) {
	t.Helper()
	b, _ := json.Marshal(ws.Up{Op: op, Topics: topics})
	if err := c.WriteText(b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readDown decodes into a map on purpose: the browser reads field names, not
// Go structs, so the names are what must be pinned.
func readDown(t *testing.T, c *ws.Conn) map[string]any {
	t.Helper()
	c.NetConn().SetReadDeadline(time.Now().Add(3 * time.Second))
	op, raw, err := c.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if op != ws.OpText {
		t.Fatalf("opcode = %d, want text", op)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("down frame %q is not JSON: %v", raw, err)
	}
	for _, k := range []string{"topic", "type", "ts", "data"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("down frame %q is missing %q", raw, k)
		}
	}
	return m
}

func TestHubRoundTrip(t *testing.T) {
	topic := ws.TickTopic("kuru", "0xABC")
	if topic != "tick.kuru.0xabc" {
		t.Fatalf("topic = %q", topic)
	}

	t.Run("update reaches a subscriber", func(t *testing.T) {
		hub, c := dial(t)
		sendUp(t, c, "subscribe", topic)
		// the subscribe has to land before the publish, and the hub is the only
		// thing that can tell us it did: publish until a frame comes back.
		go func() {
			for range 50 {
				hub.PublishJSON(topic, map[string]string{"price": "1000000000000000000"})
				time.Sleep(20 * time.Millisecond)
			}
		}()
		m := readDown(t, c)
		// a publish that beat the subscribe is stored and replayed as the
		// snapshot; the live update is the frame after it
		if m["type"] == ws.TypeSnapshot {
			m = readDown(t, c)
		}
		if m["topic"] != topic {
			t.Errorf("topic = %v", m["topic"])
		}
		if m["type"] != ws.TypeUpdate {
			t.Errorf("type = %v, want update", m["type"])
		}
		data, ok := m["data"].(map[string]any)
		if !ok || data["price"] != "1000000000000000000" {
			t.Errorf("data = %v", m["data"])
		}
	})

	t.Run("late subscriber gets the stored snapshot", func(t *testing.T) {
		hub, c := dial(t)
		hub.PublishJSON(topic, map[string]string{"price": "42"})
		sendUp(t, c, "subscribe", topic)
		m := readDown(t, c)
		if m["type"] != ws.TypeSnapshot {
			t.Errorf("type = %v, want snapshot", m["type"])
		}
	})

	t.Run("bad topic answers with an error frame", func(t *testing.T) {
		_, c := dial(t)
		sendUp(t, c, "subscribe", "tick.kuru")
		m := readDown(t, c)
		if m["type"] != ws.TypeError {
			t.Errorf("type = %v, want error", m["type"])
		}
	})

	t.Run("unsubscribe stops delivery", func(t *testing.T) {
		hub, c := dial(t)
		hub.PublishJSON(topic, map[string]string{"price": "1"})
		sendUp(t, c, "subscribe", topic)
		readDown(t, c) // snapshot
		sendUp(t, c, "unsubscribe", topic)
		sendUp(t, c, "subscribe", "index.0xdead") // the hub applies these in order
		go func() {
			for range 50 {
				hub.PublishJSON(topic, map[string]string{"price": "2"})
				hub.PublishJSON("index.0xdead", map[string]string{"value": "7"})
				time.Sleep(20 * time.Millisecond)
			}
		}()
		// drain until the first index frame: that one proves the unsubscribe
		// ordered before it was applied. every frame after it must be index.
		for readDown(t, c)["topic"] != "index.0xdead" {
		}
		for range 3 {
			if m := readDown(t, c); m["topic"] != "index.0xdead" {
				t.Fatalf("got a frame for %v after unsubscribing from it", m["topic"])
			}
		}
	})
}

func TestValidTopic(t *testing.T) {
	cases := []struct {
		topic string
		want  bool
	}{
		{"book.kuru.0xabc", true},
		{"tick.perpl.16", true},
		{ws.CandleTopic("0xABC", 3600), true},
		{ws.IndexTopic("0xABC"), true},
		{"candle.0xabc.zero", false},
		{"candle.0xabc.0", false},
		{"book.kuru", false},
		{"index.0xabc.1", false},
		{"tick..16", false},
		{"", false},
		{"orders.kuru.16", false},
	}
	for _, tc := range cases {
		if got := ws.ValidTopic(tc.topic); got != tc.want {
			t.Errorf("ValidTopic(%q) = %v, want %v", tc.topic, got, tc.want)
		}
	}
}
