package pubsub

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	wsql "github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoisonTopic parks messages that still fail after retries give up, on every
// provider (a watermill poison topic here; a dead-letter topic on gcp), so
// one bad message can never block a consumer.
const PoisonTopic = "pubsub.poison"

// PostgresOptions configures the Postgres bus.
type PostgresOptions struct {
	// PollInterval is how often idle subscribers poll for new messages.
	PollInterval time.Duration
	// LogEvents wires a dev-logger consumer group printing every message on
	// every topic (local dev visibility; this provider only).
	LogEvents bool
	// Topics is every topic the app publishes, for deterministic table
	// creation at Start and the dev-logger fanout.
	Topics []string
	// Logger defaults to a watermill adapter over the slog default logger.
	Logger watermill.LoggerAdapter
}

// PostgresBus is the watermill-sql implementation of Bus: durable messages in
// per-topic tables, fanout via per-(topic, consumer group) offsets, polling
// delivery (Neon-safe, bare-metal-safe).
type PostgresBus struct {
	pool   *pgxpool.Pool
	opts   PostgresOptions
	pub    *PostgresPublisher
	router *message.Router
}

// NewPostgresBus builds the bus. Publishing works immediately; consumers come
// online with Start.
func NewPostgresBus(pool *pgxpool.Pool, opts PostgresOptions) (*PostgresBus, error) {
	if opts.Logger == nil {
		opts.Logger = watermill.NewSlogLogger(slog.Default())
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = time.Second
	}
	pub, err := NewPostgresPublisher(pool, opts.Logger)
	if err != nil {
		return nil, err
	}
	return &PostgresBus{pool: pool, opts: opts, pub: pub}, nil
}

// Publish stores the message durably (one INSERT into the topic's table).
func (b *PostgresBus) Publish(ctx context.Context, msg Message) error {
	return b.pub.Publish(ctx, msg)
}

// PushHandler is nil: this provider pulls for itself.
func (b *PostgresBus) PushHandler() http.Handler { return nil }

// Start creates the per-topic tables, wires every subscription into a
// watermill router (one shared subscriber per consumer group), and runs it.
// Middleware order (first added = outermost): a handler error is retried with
// backoff (Recoverer turns panics into errors first); when retries are
// exhausted the message is parked on PoisonTopic and acked.
func (b *PostgresBus) Start(ctx context.Context, subs []Subscription) error {
	router, err := message.NewRouter(message.RouterConfig{}, b.opts.Logger)
	if err != nil {
		return err
	}
	poison, err := middleware.PoisonQueue(b.pub.pub, PoisonTopic)
	if err != nil {
		return err
	}
	router.AddMiddleware(poison)
	router.AddMiddleware(middleware.Retry{
		MaxRetries:      5,
		InitialInterval: time.Second,
		MaxInterval:     time.Minute,
		Multiplier:      2,
		Logger:          b.opts.Logger,
	}.Middleware)
	router.AddMiddleware(middleware.Recoverer)

	// One shared watermill subscriber per consumer group: subscriptions in the
	// same group share one delivery offset per topic.
	groupSubs := map[string]*wsql.Subscriber{}
	subscriberFor := func(group string) (*wsql.Subscriber, error) {
		if s, ok := groupSubs[group]; ok {
			return s, nil
		}
		s, err := NewPostgresSubscriber(b.pool, group, b.opts.PollInterval, b.opts.Logger)
		if err != nil {
			return nil, err
		}
		groupSubs[group] = s
		return s, nil
	}

	first := true
	for _, sub := range subs {
		gs, err := subscriberFor(sub.Group)
		if err != nil {
			return err
		}
		// Deterministic schema init, once, before any subscription runs:
		// create every topic's tables (idempotent, like store.Migrate) so
		// publishers never race table creation mid-request.
		if first {
			if err := InitializeTopics(gs, b.opts.Topics...); err != nil {
				return err
			}
			first = false
		}
		router.AddConsumerHandler(sub.Name, sub.Topic, gs, watermillHandler(sub.Topic, sub.Handle))
	}

	// Dev logger group: independently receives every message on every topic.
	if b.opts.LogEvents {
		gs, err := subscriberFor("dev-logger")
		if err != nil {
			return err
		}
		for _, topic := range b.opts.Topics {
			router.AddConsumerHandler("log-"+topic, topic, gs, watermillHandler(topic,
				func(ctx context.Context, msg Message) error {
					slog.InfoContext(ctx, "pubsub message", "topic", msg.Topic, "payload", string(msg.Payload))
					return nil
				}))
		}
	}

	b.router = router
	go func() {
		if err := router.Run(ctx); err != nil {
			slog.Error("pubsub router stopped", "error", err)
		}
	}()
	<-router.Running()
	return nil
}

// Close drains in-flight handlers and releases the publisher.
func (b *PostgresBus) Close() error {
	var errs []error
	if b.router != nil {
		if err := b.router.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := b.pub.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("pubsub: close postgres bus: %v", errs)
	}
	return nil
}
