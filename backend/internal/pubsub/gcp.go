package pubsub

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	gpubsub "cloud.google.com/go/pubsub/v2"
	"google.golang.org/api/idtoken"
)

// GCPOptions configures the Google Cloud Pub/Sub bus.
type GCPOptions struct {
	// ProjectID is the GCP project owning the topics/subscriptions (created by
	// infra, never by the app).
	ProjectID string
	// PushAudience is the OIDC audience expected on push deliveries — the push
	// endpoint URL as configured on the subscriptions.
	PushAudience string
	// PushServiceAccount is the service account email Pub/Sub signs push
	// tokens with; deliveries from any other principal are rejected.
	PushServiceAccount string
	// Verifier validates push OIDC tokens. Nil selects the Google-backed
	// default; tests inject a fake.
	Verifier TokenVerifier
}

// TokenVerifier validates a push delivery's OIDC token and returns the
// authenticated service-account email.
type TokenVerifier interface {
	Verify(ctx context.Context, token, audience string) (email string, err error)
}

// googleVerifier is the production TokenVerifier, backed by Google's JWKS.
type googleVerifier struct{}

func (googleVerifier) Verify(ctx context.Context, token, audience string) (string, error) {
	payload, err := idtoken.Validate(ctx, token, audience)
	if err != nil {
		return "", err
	}
	email, _ := payload.Claims["email"].(string)
	if verified, _ := payload.Claims["email_verified"].(bool); !verified || email == "" {
		return "", fmt.Errorf("token has no verified email claim")
	}
	return email, nil
}

// GCPBus is the Google Cloud Pub/Sub implementation of Bus. Publishing goes
// through the Pub/Sub client; consuming is push-based — Pub/Sub POSTs each
// message to PushHandler (mounted at PushPath), so nothing polls and nothing
// stays awake between events. Fanout is native: every subscription on a topic
// receives every message, and retry/dead-letter policy lives on the
// subscription (infra), not in Go.
type GCPBus struct {
	client *gpubsub.Client
	opts   GCPOptions

	mu         sync.Mutex
	publishers map[string]*gpubsub.Publisher
	push       *pushServer
}

// NewGCPBus connects the Pub/Sub client. Topics must already exist.
func NewGCPBus(ctx context.Context, opts GCPOptions) (*GCPBus, error) {
	if opts.ProjectID == "" || opts.PushAudience == "" || opts.PushServiceAccount == "" {
		return nil, fmt.Errorf("pubsub: gcp provider requires project_id, push_audience, and push_service_account")
	}
	if opts.Verifier == nil {
		opts.Verifier = googleVerifier{}
	}
	client, err := gpubsub.NewClient(ctx, opts.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("pubsub: gcp client: %w", err)
	}
	return &GCPBus{client: client, opts: opts, publishers: map[string]*gpubsub.Publisher{}}, nil
}

// Publish sends the message and waits for the server ack, so callers (webhook
// handlers) see a real error and can 5xx for provider retry.
func (b *GCPBus) Publish(ctx context.Context, msg Message) error {
	res := b.publisher(msg.Topic).Publish(ctx, &gpubsub.Message{
		Data:       msg.Payload,
		Attributes: msg.Attributes,
	})
	if _, err := res.Get(ctx); err != nil {
		return fmt.Errorf("pubsub: publish %s: %w", msg.Topic, err)
	}
	return nil
}

// publisher returns the cached per-topic publisher (they hold batching state
// and must be reused, not recreated per publish).
func (b *GCPBus) publisher(topic string) *gpubsub.Publisher {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p, ok := b.publishers[topic]; ok {
		return p
	}
	p := b.client.Publisher(topic)
	b.publishers[topic] = p
	return p
}

// Start registers the subscriptions with the push dispatcher. No network work:
// the subscriptions themselves exist in infra, and delivery arrives over
// HTTP once PushHandler is mounted.
func (b *GCPBus) Start(_ context.Context, subs []Subscription) error {
	byName := make(map[string]Subscription, len(subs))
	for _, sub := range subs {
		if _, dup := byName[sub.Name]; dup {
			return fmt.Errorf("pubsub: duplicate subscription name %q", sub.Name)
		}
		byName[sub.Name] = sub
	}
	b.push = &pushServer{
		subs:      byName,
		verify:    b.opts.Verifier,
		audience:  b.opts.PushAudience,
		wantEmail: b.opts.PushServiceAccount,
	}
	return nil
}

// PushHandler serves Pub/Sub push deliveries. Mount at PushPath. Nil until
// Start has registered the subscriptions (a typed-nil *pushServer inside the
// interface would defeat the caller's nil check).
func (b *GCPBus) PushHandler() http.Handler {
	if b.push == nil {
		return nil
	}
	return b.push
}

// Close flushes pending publishes and releases the client.
func (b *GCPBus) Close() error {
	b.mu.Lock()
	for _, p := range b.publishers {
		p.Stop() // flush
	}
	b.mu.Unlock()
	return b.client.Close()
}