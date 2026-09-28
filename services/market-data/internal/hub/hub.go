// Package hub fans publisher streams out to WebSocket clients: one Redis reader per active topic.
package hub

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/snehendu098/sweem-terminal/internal/streams"
)

const (
	MaxTopics  = 32
	sendBuffer = 64 // a client this far behind is dropped
	pingEvery  = 20 * time.Second
	writeWait  = 10 * time.Second
)

var topicRE = regexp.MustCompile(`^(book|nav|trades)\.0x[0-9a-f]{40}$`)

func ValidTopic(t string) bool { return topicRE.MatchString(t) }

// Client is one connection's outbox. drop must be safe to call repeatedly and must not call back into the hub.
type Client struct {
	send chan []byte
	drop func()
}

type topic struct {
	subs   map[*Client]struct{}
	last   []byte // latest snapshot frame, replayed to new subscribers (book/nav only)
	cancel context.CancelFunc
}

type Hub struct {
	ctx    context.Context
	mu     sync.Mutex
	topics map[string]*topic
	// run follows one topic until ctx ends, calling emit per message. Swapped in tests.
	run func(ctx context.Context, topic string, emit func(any)) error
}

func New(ctx context.Context, rdb *redis.Client) *Hub {
	return &Hub{ctx: ctx, topics: map[string]*topic{}, run: func(ctx context.Context, t string, emit func(any)) error {
		kind, addr, _ := strings.Cut(t, ".")
		switch kind {
		case "book":
			return streams.Reader[streams.BookSnapshot]{RDB: rdb, Stream: streams.Book(addr), Latest: streams.BookLatest(addr),
				Seq: func(b streams.BookSnapshot) uint64 { return b.Seq }}.Run(ctx, func(v streams.BookSnapshot) { emit(v) })
		case "nav":
			return streams.Reader[streams.NavPoint]{RDB: rdb, Stream: streams.Nav(addr), Latest: streams.NavLatest(addr)}.
				Run(ctx, func(v streams.NavPoint) { emit(v) })
		default:
			return streams.Reader[streams.Trade]{RDB: rdb, Stream: streams.Trades(addr)}.Run(ctx, func(v streams.Trade) { emit(v) })
		}
	}}
}

func (h *Hub) Sub(c *Client, t string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tp := h.topics[t]
	if tp == nil {
		ctx, cancel := context.WithCancel(h.ctx)
		tp = &topic{subs: map[*Client]struct{}{}, cancel: cancel}
		h.topics[t] = tp
		go h.follow(ctx, t)
	}
	if _, ok := tp.subs[c]; ok {
		return
	}
	tp.subs[c] = struct{}{}
	if tp.last != nil {
		c.push(tp.last)
	}
}

func (h *Hub) Unsub(c *Client, t string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tp := h.topics[t]
	if tp == nil {
		return
	}
	delete(tp.subs, c)
	if len(tp.subs) == 0 {
		tp.cancel()
		delete(h.topics, t)
	}
}

func (h *Hub) follow(ctx context.Context, t string) {
	emit := func(v any) {
		b, err := json.Marshal(map[string]any{"topic": t, "data": v})
		if err != nil {
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		tp := h.topics[t]
		if tp == nil || ctx.Err() != nil {
			return
		}
		if !strings.HasPrefix(t, "trades.") {
			tp.last = b
		}
		for c := range tp.subs {
			c.push(b)
		}
	}
	for ctx.Err() == nil {
		if err := h.run(ctx, t, emit); err != nil && ctx.Err() == nil {
			slog.Warn("topic reader", "topic", t, "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}
}

func (c *Client) push(b []byte) {
	select {
	case c.send <- b:
	default:
		c.drop()
	}
}

// Serve upgrades to WebSocket and runs the sub/unsub protocol until the client leaves or falls behind.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request) {
	// public read-only data, so any origin may connect
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4096)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &Client{send: make(chan []byte, sendBuffer), drop: cancel}
	subs := map[string]bool{}
	defer func() {
		for t := range subs {
			h.Unsub(c, t)
		}
	}()

	go func() {
		ping := time.NewTicker(pingEvery)
		defer ping.Stop()
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-c.send:
				wctx, wc := context.WithTimeout(ctx, writeWait)
				err := conn.Write(wctx, websocket.MessageText, b)
				wc()
				if err != nil {
					return
				}
			case <-ping.C:
				pctx, pc := context.WithTimeout(ctx, writeWait)
				err := conn.Ping(pctx)
				pc()
				if err != nil {
					return
				}
			}
		}
	}()

	reply := func(v any) {
		b, _ := json.Marshal(v)
		c.push(b)
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg struct {
			Op     string   `json:"op"`
			Topics []string `json:"topics"`
		}
		if json.Unmarshal(data, &msg) != nil || (msg.Op != "sub" && msg.Op != "unsub") {
			reply(map[string]string{"error": "bad_request"})
			continue
		}
		for _, t := range msg.Topics {
			switch {
			case !ValidTopic(t):
				reply(map[string]string{"error": "bad_topic", "topic": t})
			case msg.Op == "unsub":
				if subs[t] {
					delete(subs, t)
					h.Unsub(c, t)
				}
			case subs[t]:
			case len(subs) >= MaxTopics:
				reply(map[string]string{"error": "too_many_topics", "topic": t})
			default:
				subs[t] = true
				h.Sub(c, t)
			}
		}
	}
}
