package ws

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// ponytail: 256-frame per-client queue, drop the client when it overflows.
// swap for per-topic conflation if a browser ever legitimately falls behind.
const sendQueue = 256

type client struct {
	conn   *Conn
	send   chan []byte
	mu     sync.Mutex
	topics map[string]bool
}

// Hub fans topic payloads out to browsers. It keeps the last frame per topic so
// a late subscriber gets a snapshot instead of waiting for the next tick.
type Hub struct {
	mu      sync.RWMutex
	clients map[*client]bool
	last    map[string][]byte
}

func NewHub() *Hub {
	return &Hub{clients: map[*client]bool{}, last: map[string][]byte{}}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := Accept(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c := &client{conn: conn, send: make(chan []byte, sendQueue), topics: map[string]bool{}}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	go h.writeLoop(c)
	h.readLoop(c)
}

func (h *Hub) readLoop(c *client) {
	defer h.drop(c)
	for {
		op, msg, err := c.conn.Read()
		if err != nil {
			return
		}
		if op != OpText {
			continue
		}
		var up Up
		if err := json.Unmarshal(msg, &up); err != nil {
			h.send(c, frame("", TypeError, jsonString("frame was not valid JSON")))
			continue
		}
		for _, t := range up.Topics {
			if !ValidTopic(t) {
				h.send(c, frame(t, TypeError, jsonString("topic is not a known market-data topic")))
				continue
			}
			switch up.Op {
			case "subscribe":
				c.mu.Lock()
				c.topics[t] = true
				c.mu.Unlock()
				h.mu.RLock()
				snap := h.last[t]
				h.mu.RUnlock()
				if snap != nil {
					h.send(c, frame(t, TypeSnapshot, snap))
				}
			case "unsubscribe":
				c.mu.Lock()
				delete(c.topics, t)
				c.mu.Unlock()
			default:
				h.send(c, frame(t, TypeError, jsonString("op must be subscribe or unsubscribe")))
			}
		}
	}
}

func (h *Hub) writeLoop(c *client) {
	for b := range c.send {
		if err := c.conn.WriteText(b); err != nil {
			h.drop(c)
			return
		}
	}
}

func (h *Hub) drop(c *client) {
	h.mu.Lock()
	if !h.clients[c] {
		h.mu.Unlock()
		return
	}
	delete(h.clients, c)
	h.mu.Unlock()
	close(c.send)
	c.conn.Close()
}

func (h *Hub) send(c *client, b []byte) {
	defer func() { recover() }() // send on a closed queue means the client is already gone
	select {
	case c.send <- b:
	default:
		slog.Warn("ws client fell behind, dropping")
		go h.drop(c)
	}
}

// Publish stores payload as the topic's snapshot and pushes it to subscribers.
func (h *Hub) Publish(topic string, payload []byte) {
	h.mu.Lock()
	h.last[topic] = payload
	h.mu.Unlock()
	b := frame(topic, TypeUpdate, payload)
	h.mu.RLock()
	targets := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		c.mu.Lock()
		sub := c.topics[topic]
		c.mu.Unlock()
		if sub {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range targets {
		h.send(c, b)
	}
}

func (h *Hub) PublishJSON(topic string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("marshal ws payload", "topic", topic, "err", err)
		return
	}
	h.Publish(topic, b)
}

func frame(topic, typ string, data []byte) []byte {
	b, err := json.Marshal(Down{Topic: topic, Type: typ, Ts: time.Now().UnixMilli(), Data: data})
	if err != nil {
		return []byte(`{"topic":"","type":"error","data":"frame could not be encoded"}`)
	}
	return b
}

func jsonString(s string) []byte {
	b, _ := json.Marshal(s)
	return b
}
