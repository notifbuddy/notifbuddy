package integrations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"log"
	"net/http"

	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/store"
)

// GitHubWebhookTopic is the logical topic fired for each GitHub webhook we
// receive. Backends (memory/SNS) map it to a concrete destination.
const GitHubWebhookTopic = "integrations.github.webhook_event"

const maxWebhookBody = 5 << 20 // 5 MiB, generous for GitHub payloads

// githubWebhookEvent is the published event shape (also what subscribers see).
type githubWebhookEvent struct {
	DeliveryID     string `json:"delivery_id"`
	EventType      string `json:"event_type"`
	Action         string `json:"action,omitempty"`
	InstallationID string `json:"installation_id,omitempty"`
	OrgID          string `json:"org_id,omitempty"`
}

// HandleGitHubWebhook receives a GitHub webhook delivery. It performs two
// deliberately separate operations:
//
//  1. Verify the HMAC signature, then STORE the event durably (source of truth).
//  2. PUBLISH integrations.github.webhook_event as a notification.
//
// Publishing is best-effort: if it fails we log and still return 202, because
// the event is already stored and we don't want GitHub to redeliver just because
// the notification fan-out hiccuped. The two ops are never merged.
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
			log.Printf("integrations: github webhook signature mismatch (delivery %s)", r.Header.Get("X-GitHub-Delivery"))
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

	// Resolve which org owns this installation (best-effort).
	orgID := ""
	if installationID != "" {
		if id, err := s.store.OrgIDByGitHubInstallation(r.Context(), installationID); err == nil {
			orgID = id
		}
	}

	// 1b. STORE (op 1) — durable source of truth. Idempotent on delivery id.
	inserted, err := s.store.InsertGitHubWebhookEvent(r.Context(), store.GitHubWebhookEvent{
		DeliveryID:     deliveryID,
		EventType:      eventType,
		InstallationID: installationID,
		OrgID:          orgID,
		Action:         parsed.Action,
		Payload:        json.RawMessage(body),
	})
	if err != nil {
		log.Printf("integrations: store github webhook %s: %v", deliveryID, err)
		http.Error(w, "failed to store event", http.StatusInternalServerError)
		return
	}
	if !inserted {
		// Redelivery of an already-stored event: ack without re-publishing.
		w.WriteHeader(http.StatusOK)
		return
	}

	// 2. PUBLISH (op 2) — separate, best-effort notification.
	s.publishGitHubWebhook(r, githubWebhookEvent{
		DeliveryID:     deliveryID,
		EventType:      eventType,
		Action:         parsed.Action,
		InstallationID: installationID,
		OrgID:          orgID,
	})

	w.WriteHeader(http.StatusAccepted)
}

// publishGitHubWebhook fires integrations.github.webhook_event. Failures are
// logged, not surfaced — the event is already stored.
func (s *Service) publishGitHubWebhook(r *http.Request, evt githubWebhookEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		log.Printf("integrations: marshal webhook event %s: %v", evt.DeliveryID, err)
		return
	}
	msg := pubsub.Message{
		Topic:   GitHubWebhookTopic,
		Payload: payload,
		Attributes: map[string]string{
			"event_type": evt.EventType,
			"org_id":     evt.OrgID,
		},
	}
	if err := s.pub.Publish(r.Context(), msg); err != nil {
		log.Printf("integrations: publish %s for delivery %s failed (event stored): %v",
			GitHubWebhookTopic, evt.DeliveryID, err)
	}
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
