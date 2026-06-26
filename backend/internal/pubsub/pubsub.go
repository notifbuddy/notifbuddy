// Package pubsub is a provider-agnostic publish interface. Callers depend only
// on Publisher and Message; concrete backends (in-memory for local dev, SNS for
// production) live in their own files and never leak their types — no aws-sdk or
// channel types appear in this package's public surface.
//
// Topics are dotted strings, e.g. "integrations.github.webhook_event".
package pubsub

import "context"

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
