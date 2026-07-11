package integrations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"

	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/store"
)

// GitHubWebhookReceivedTopic carries each verified raw GitHub delivery from
// the HTTP receiver to the writer consumer (payload = raw webhook body).
const GitHubWebhookReceivedTopic = "integrations.github.webhook.received"

// GitHubWebhookTopic is the processed topic the writer fires once a delivery
// is persisted.
const GitHubWebhookTopic = "integrations.github.webhook_event"

const maxWebhookBody = 5 << 20 // 5 MiB, generous for GitHub payloads

// githubWebhookEvent is the envelope published on GitHubWebhookTopic.
type githubWebhookEvent struct {
	DeliveryID     string `json:"delivery_id"`
	EventType      string `json:"event_type"`
	Action         string `json:"action,omitempty"`
	InstallationID string `json:"installation_id,omitempty"`
	OrgID          string `json:"org_id,omitempty"`
}

// HandleGitHubWebhook receives a GitHub webhook delivery. It only verifies the
// HMAC signature and publishes the raw body durably on
// integrations.github.webhook.received; persistence (and dedup on delivery id)
// happens in the writer consumer. A publish failure returns 5xx so GitHub
// redelivers.
func (s *Service) HandleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		http.Error(w, "integrations not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	// 1a. Verify signature. If a webhook secret is configured, it must match.
	if secret := s.cfg.GitHub.WebhookSecret; secret != "" {
		if !validGitHubSignature(secret, body, r.Header.Get("X-Hub-Signature-256")) {
			slog.WarnContext(r.Context(), "integrations: github webhook signature mismatch", "delivery_id", r.Header.Get("X-GitHub-Delivery"))
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	deliveryID := r.Header.Get("X-GitHub-Delivery")
	eventType := r.Header.Get("X-GitHub-Event")
	if deliveryID == "" || eventType == "" {
		http.Error(w, "missing delivery headers", http.StatusBadRequest)
		return
	}

	// Pull a few fields out of the payload for indexing/display.
	var parsed struct {
		Action       string `json:"action"`
		Installation struct {
			ID json.Number `json:"id"`
		} `json:"installation"`
	}
	_ = json.Unmarshal(body, &parsed)
	installationID := parsed.Installation.ID.String()
	if installationID == "0" {
		installationID = ""
	}

	// PUBLISH the raw delivery durably; the writer consumer persists it (dedup
	// on delivery id) and fires the processed topic. A failed publish means the
	// delivery is not recorded anywhere, so surface a 5xx for GitHub to retry.
	if err := s.pub.Publish(r.Context(), pubsub.Message{
		Topic:   GitHubWebhookReceivedTopic,
		Payload: body,
		Attributes: map[string]string{
			"delivery_id":     deliveryID,
			"event_type":      eventType,
			"action":          parsed.Action,
			"installation_id": installationID,
		},
	}); err != nil {
		slog.ErrorContext(r.Context(), "integrations: publish github webhook", "delivery_id", deliveryID, "error", err)
		http.Error(w, "failed to accept event", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// WriteGitHubWebhook consumes integrations.github.webhook.received: it
// resolves the owning org, persists the delivery (idempotent on delivery id),
// and publishes the routing envelope on the processed topic. A returned error
// nacks the message for redelivery — including when the insert committed but
// the envelope publish failed, which the envelope_published flag turns into a
// publish retry instead of a lost event.
func (s *Service) WriteGitHubWebhook(ctx context.Context, msg pubsub.Message) error {
	evt := githubWebhookEvent{
		DeliveryID:     msg.Attributes["delivery_id"],
		EventType:      msg.Attributes["event_type"],
		Action:         msg.Attributes["action"],
		InstallationID: msg.Attributes["installation_id"],
	}

	// Resolve which org owns this installation (best-effort).
	if evt.InstallationID != "" {
		if id, err := s.store.OrgIDByGitHubInstallation(ctx, evt.InstallationID); err == nil {
			evt.OrgID = id
		}
	}

	inserted, published, err := s.store.InsertGitHubWebhookEvent(ctx, store.GitHubWebhookEvent{
		DeliveryID:     evt.DeliveryID,
		EventType:      evt.EventType,
		InstallationID: evt.InstallationID,
		OrgID:          evt.OrgID,
		Action:         evt.Action,
		Payload:        json.RawMessage(msg.Payload),
	})
	if err != nil {
		return fmt.Errorf("store github webhook %s: %w", evt.DeliveryID, err)
	}
	if !inserted && published {
		return nil // redelivery of a fully-processed delivery: consume silently
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal github envelope %s: %w", evt.DeliveryID, err)
	}
	if err := s.pub.Publish(ctx, pubsub.Message{
		Topic:   GitHubWebhookTopic,
		Payload: payload,
		Attributes: map[string]string{
			"event_type": evt.EventType,
			"org_id":     evt.OrgID,
		},
	}); err != nil {
		return fmt.Errorf("publish github envelope %s: %w", evt.DeliveryID, err)
	}
	// Failure here only risks a duplicate envelope on a later redelivery,
	// which downstream consumers must tolerate anyway (at-least-once).
	if err := s.store.MarkGitHubWebhookPublished(ctx, evt.DeliveryID); err != nil {
		slog.ErrorContext(ctx, "integrations: mark github webhook published", "delivery_id", evt.DeliveryID, "error", err)
	}
	return nil
}

// validGitHubSignature checks the X-Hub-Signature-256 header (format
// "sha256=<hex>") against an HMAC-SHA256 of the body keyed by the webhook secret,
// using a constant-time comparison.
func validGitHubSignature(secret string, body []byte, header string) bool {
	const prefix = "sha256="
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return false
	}
	want, err := hex.DecodeString(header[len(prefix):])
	if err != nil {
		return false
	}
	var mac hash.Hash = hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(want, mac.Sum(nil))
}

// WebhookEvent is the trimmed view of a stored webhook event for the API.
type WebhookEvent struct {
	DeliveryID string
	EventType  string
	Action     string
	ReceivedAt string
	Payload    json.RawMessage
}

// ListGitHubWebhooks returns an org's most recent stored webhook events.
func (s *Service) ListGitHubWebhooks(ctx context.Context, orgID string, limit int) ([]WebhookEvent, error) {
	if !s.Enabled() || orgID == "" {
		return nil, nil
	}
	rows, err := s.store.ListGitHubWebhookEvents(ctx, orgID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]WebhookEvent, 0, len(rows))
	for _, e := range rows {
		out = append(out, WebhookEvent{
			DeliveryID: e.DeliveryID,
			EventType:  e.EventType,
			Action:     e.Action,
			ReceivedAt: e.ReceivedAt,
			Payload:    e.Payload,
		})
	}
	return out, nil
}
