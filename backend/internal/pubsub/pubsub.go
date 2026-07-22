// Package pubsub is the provider-agnostic eventing layer. Publishers and
// consumers depend only on Message, Publisher, Handler, and Subscription;
// concrete backends (Postgres/watermill for local and bare-metal deploys,
// Google Cloud Pub/Sub push for Cloud Run) live in their own files and never
// leak provider types into consumer code. The backend is selected by
// config.yaml (pubsub.provider) via NewBus.
//
// Semantics common to every provider: publish once → every Subscription on
// the topic receives the message (fanout), delivery is at-least-once, and a
// Handler error nacks only that message on that subscription — retries are
// per-consumer, and exhausted retries park the message on the pubsub.poison
// dead-letter topic.
//
// Topics are dotted strings, e.g. "integrations.github.webhook_event".
package pubsub

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"xolo/backend/internal/config"
)

// Message is a single event to publish. Payload is the already-serialized body
// (typically JSON). Attributes are optional string metadata that backends may
// map to provider features (e.g. SNS message attributes) for filtering/routing.
type Message struct {
	Topic      string
	Payload    []byte
	Attributes map[string]string
}

// Publisher publishes messages to a topic. Implementations must be safe for
// concurrent use. Publish should be treated as best-effort by callers that have
// already durably recorded the underlying event elsewhere.
type Publisher interface {
	Publish(ctx context.Context, msg Message) error
}

// PublisherFunc adapts a function to the Publisher interface (handy for tests).
type PublisherFunc func(ctx context.Context, msg Message) error

// Publish calls the underlying function.
func (f PublisherFunc) Publish(ctx context.Context, msg Message) error { return f(ctx, msg) }

// Nop is a Publisher that discards everything. Used when publishing is disabled.
var Nop Publisher = PublisherFunc(func(context.Context, Message) error { return nil })

// Handler consumes one message. A returned error nacks the message for
// redelivery (with provider-specific backoff, then dead-lettering) — return
// nil for permanent skips, an error only when a retry could succeed.
type Handler func(ctx context.Context, msg Message) error

// Subscription declares one consumer of one topic. Name is the stable
// consumer identity: the watermill handler name on postgres and the
// subscription ID on gcp (it must match infra-created subscription).
// Group is the postgres consumer group — subscriptions sharing a Group share
// one delivery offset per topic; gcp ignores it (every subscription is its
// own group by construction).
type Subscription struct {
	Name   string
	Group  string
	Topic  string
	Handle Handler
}

// Bus is a provider-agnostic eventing runtime: a durable Publisher plus the
// consumer side of the registered subscriptions.
type Bus interface {
	Publisher
	// Start brings the consumers online; it returns once they are live.
	Start(ctx context.Context, subs []Subscription) error
	// Close flushes publishers and drains consumers.
	Close() error
	// PushHandler returns the HTTP endpoint push-based providers deliver to
	// (mount at PushPath), or nil for providers that pull for themselves.
	PushHandler() http.Handler
}

// PushPath is where main.go mounts PushHandler for push-based providers; it
// must match the push endpoint configured on the provider's subscriptions.
const PushPath = "/internal/pubsub/push"

// NewBus constructs the Bus selected by cfg.Provider. The postgres provider
// requires the shared pgx pool; gcp ignores it. Topics come from
// manifest.yaml, the topology file shared with infra.
func NewBus(ctx context.Context, cfg config.PubSubConfig, pool *pgxpool.Pool) (Bus, error) {
	switch cfg.Provider {
	case "", "postgres":
		if pool == nil {
			return nil, fmt.Errorf("pubsub: provider postgres requires a database")
		}
		topics, err := Topics()
		if err != nil {
			return nil, err
		}
		return NewPostgresBus(pool, PostgresOptions{
			PollInterval: cfg.Postgres.PollIntervalDuration(),
			LogEvents:    cfg.Postgres.LogEvents,
			Topics:       topics,
		})
	case "gcp":
		return NewGCPBus(ctx, GCPOptions{
			ProjectID:          cfg.GCP.ProjectID,
			PushAudience:       cfg.GCP.PushAudience,
			PushServiceAccount: cfg.GCP.PushServiceAccount,
			NamePrefix:         cfg.GCP.NamePrefix,
		})
	default:
		return nil, fmt.Errorf("pubsub: unknown provider %q (want postgres or gcp)", cfg.Provider)
	}
}
