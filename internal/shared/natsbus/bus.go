package natsbus

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamName = "SWEEM"
	retention  = 7 * 24 * time.Hour
	maxDeliver = 5
	// ponytail: 2h dedup window, widen if a producer can retry later than that
	dupWindow = 2 * time.Hour
)

var streamSubjects = []string{"execution.>"}

type Bus struct {
	nc *nats.Conn
	js jetstream.JetStream
}

func New(url string) (*Bus, error) {
	nc, err := nats.Connect(url, nats.MaxReconnects(-1), nats.ReconnectWait(time.Second))
	if err != nil {
		return nil, fmt.Errorf("connect nats at %s: %w", url, err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("init jetstream: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       StreamName,
		Subjects:   streamSubjects,
		Retention:  jetstream.LimitsPolicy,
		MaxAge:     retention,
		Duplicates: dupWindow,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("create stream %s: %w", StreamName, err)
	}
	return &Bus{nc: nc, js: js}, nil
}

func (b *Bus) Close() { b.nc.Close() }

// idempotencyKey is the dedup id: a replayed publish within the window is dropped
func (b *Bus) Publish(ctx context.Context, subject, idempotencyKey string, payload []byte) error {
	opts := []jetstream.PublishOpt{}
	if idempotencyKey != "" {
		opts = append(opts, jetstream.WithMsgID(idempotencyKey))
	}
	if _, err := b.js.Publish(ctx, subject, payload, opts...); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// Subscribe starts a durable pull consumer and returns once it is running.
// Delivery stops when ctx is cancelled. fn returning an error naks with backoff.
func (b *Bus) Subscribe(ctx context.Context, subject, durable string, fn func([]byte) error) error {
	stream, err := b.js.Stream(ctx, StreamName)
	if err != nil {
		return fmt.Errorf("open stream %s: %w", StreamName, err)
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       durable,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    maxDeliver,
		AckWait:       30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("create consumer %s on %s: %w", durable, subject, err)
	}
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		if err := fn(msg.Data()); err != nil {
			attempt := uint64(1)
			if md, mdErr := msg.Metadata(); mdErr == nil {
				attempt = md.NumDelivered
			}
			slog.Warn("handler failed, naking", "subject", msg.Subject(), "durable", durable, "attempt", attempt, "err", err)
			msg.NakWithDelay(backoff(attempt))
			return
		}
		if err := msg.Ack(); err != nil {
			slog.Error("ack failed", "subject", msg.Subject(), "durable", durable, "err", err)
		}
	})
	if err != nil {
		return fmt.Errorf("consume %s: %w", subject, err)
	}
	go func() {
		<-ctx.Done()
		cc.Stop()
	}()
	return nil
}

func backoff(attempt uint64) time.Duration {
	if attempt > maxDeliver {
		attempt = maxDeliver
	}
	d := time.Second << (attempt - 1)
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}
