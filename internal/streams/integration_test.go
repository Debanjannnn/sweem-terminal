//go:build integration

package streams

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func rdb(t *testing.T) *redis.Client {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:6380"
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opt)
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestReaderGapAndSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := rdb(t)
	m := "0xTEST" + uuid.NewString()[:8]
	t.Cleanup(func() { c.Del(context.Background(), Book(m), BookLatest(m)) })
	pub := func(stream, latest string, seq uint64) {
		if err := Publish(ctx, c, stream, latest, BookSnapshot{Market: Addr(m), Seq: seq}); err != nil {
			t.Fatal(err)
		}
	}
	pub(Book(m), BookLatest(m), 1)

	got := make(chan uint64, 16)
	r := Reader[BookSnapshot]{RDB: c, Stream: Book(m), Latest: BookLatest(m), Seq: func(b BookSnapshot) uint64 { return b.Seq }, Block: 200 * time.Millisecond}
	go r.Run(ctx, func(b BookSnapshot) { got <- b.Seq })
	if s := <-got; s != 1 {
		t.Fatalf("snapshot seq %d", s)
	}
	pub(Book(m), BookLatest(m), 2)
	pub(Book(m), BookLatest(m), 3)
	pub("", BookLatest(m), 5) // latest moved past a message the stream never carried
	pub(Book(m), "", 6)       // stream jumps 3→6, so reader must re-read latest (5) first

	var seqs []uint64
	for len(seqs) < 4 {
		select {
		case s := <-got:
			seqs = append(seqs, s)
		case <-ctx.Done():
			t.Fatalf("timeout, got %v", seqs)
		}
	}
	if want := []uint64{2, 3, 5, 6}; !reflect.DeepEqual(seqs, want) {
		t.Fatalf("got %v want %v", seqs, want)
	}
	if n := c.XLen(ctx, Book(m)).Val(); n != 4 {
		t.Fatalf("stream len %d", n)
	}
}

func TestHeartbeat(t *testing.T) {
	ctx := context.Background()
	c := rdb(t)
	c.Del(ctx, HeartbeatKey)
	if ok, _ := Alive(ctx, c); ok {
		t.Fatal("alive without beat")
	}
	if err := Beat(ctx, c); err != nil {
		t.Fatal(err)
	}
	if ok, _ := Alive(ctx, c); !ok {
		t.Fatal("not alive after beat")
	}
	if ttl := c.TTL(ctx, HeartbeatKey).Val(); ttl <= 0 || ttl > 5*time.Second {
		t.Fatalf("ttl %v", ttl)
	}
	c.Del(ctx, HeartbeatKey)
}
