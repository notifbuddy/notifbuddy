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

// LinearWebhookTopic is the logical topic fired for each Linear webhook we
// receive. Backends (memory/SNS) map it to a concrete destination.
const LinearWebhookTopic = "integrations.linear.webhook_event"

// linearWebhookEvent is the published event shape (also what subscribers see).
type linearWebhookEvent struct {
	DeliveryID  string `json:"delivery_id"`
	EventType   string `json:"event_type"`
	Action      string `json:"action,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	OrgID       string `json:"org_id,omitempty"`
}

// HandleLinearWebhook receives a Linear webhook delivery. Like the GitHub
// receiver it performs two deliberately separate operations:
//
//  1. Verify the Linear-Signature HMAC, then STORE the event durably.
//  2. PUBLISH integrations.linear.webhook_event as a best-effort notification.
//
// Linear has no per-delivery header, so the delivery id is derived from the
// payload's webhookId + webhookTimestamp, which makes redeliveries idempotent.
func (s *Service) HandleLinearWebhook(w http.ResponseWriter, r *http.Request) {
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
	if secret := s.cfg.Linear.WebhookSecret; secret != "" {
		if !validLinearSignature(secret, body, r.Header.Get("Linear-Signature")) {
			log.Printf("integrations: linear webhook signature mismatch")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Pull the fields Linear includes in every webhook payload. webhookTimestamp
	// arrives as a JSON number (epoch ms), so decode it as json.Number rather than
	// string — unmarshalling a number into a string field would fail.
	var parsed struct {
		Action         string      `json:"action"`
		Type           string      `json:"type"`
		OrganizationID string      `json:"organizationId"`
		WebhookID      string      `json:"webhookId"`
		Timestamp      json.Number `json:"webhookTimestamp"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if parsed.Type == "" {
		http.Error(w, "missing event type", http.StatusBadRequest)
		return
	}

	// Derive a stable delivery id for idempotency. webhookTimestamp is unique per
	// delivery; pair it with webhookId. Fall back to a body hash if absent.
	deliveryID := parsed.WebhookID + ":" + parsed.Timestamp.String()
	if deliveryID == ":" {
		sum := sha256.Sum256(body)
		deliveryID = hex.EncodeToString(sum[:])
	}

	// Resolve which org owns this workspace (best-effort).
	orgID := ""
	if parsed.OrganizationID != "" {
		if id, err := s.store.OrgIDByLinearWorkspace(r.Context(), parsed.OrganizationID); err == nil {
			orgID = id
		}
	}

	// 1b. STORE (op 1) — durable source of truth. Idempotent on delivery id.
	inserted, err := s.store.InsertLinearWebhookEvent(r.Context(), store.LinearWebhookEvent{
		DeliveryID:  deliveryID,
		EventType:   parsed.Type,
		WorkspaceID: parsed.OrganizationID,
		OrgID:       orgID,
		Action:      parsed.Action,
		Payload:     json.RawMessage(body),
	})
	if err != nil {
		log.Printf("integrations: store linear webhook %s: %v", deliveryID, err)
		http.Error(w, "failed to store event", http.StatusInternalServerError)
		return
	}
	if !inserted {
		// Redelivery of an already-stored event: ack without re-publishing.
		w.WriteHeader(http.StatusOK)
		return
	}

	// 2. PUBLISH (op 2) — separate, best-effort notification.
	s.publishLinearWebhook(r, linearWebhookEvent{
		DeliveryID:  deliveryID,
		EventType:   parsed.Type,
		Action:      parsed.Action,
		WorkspaceID: parsed.OrganizationID,
		OrgID:       orgID,
	})

	w.WriteHeader(http.StatusAccepted)
}

// publishLinearWebhook fires integrations.linear.webhook_event. Failures are
// logged, not surfaced — the event is already stored.
func (s *Service) publishLinearWebhook(r *http.Request, evt linearWebhookEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		log.Printf("integrations: marshal linear webhook event %s: %v", evt.DeliveryID, err)
		return
	}
	msg := pubsub.Message{
		Topic:   LinearWebhookTopic,
		Payload: payload,
		Attributes: map[string]string{
			"event_type": evt.EventType,
			"org_id":     evt.OrgID,
		},
	}
	if err := s.pub.Publish(r.Context(), msg); err != nil {
		log.Printf("integrations: publish %s for delivery %s failed (event stored): %v",
			LinearWebhookTopic, evt.DeliveryID, err)
	}
}

// validLinearSignature checks the Linear-Signature header (a hex-encoded
// HMAC-SHA256 of the raw body keyed by the webhook secret) using a constant-time
// comparison.
func validLinearSignature(secret string, body []byte, header string) bool {
	if header == "" {
		return false
	}
	want, err := hex.DecodeString(header)
	if err != nil {
		return false
	}
	var mac hash.Hash = hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(want, mac.Sum(nil))
}

// ListLinearWebhooks returns an org's most recent stored Linear webhook events.
func (s *Service) ListLinearWebhooks(ctx context.Context, orgID string, limit int) ([]WebhookEvent, error) {
	if !s.Enabled() || orgID == "" {
		return nil, nil
	}
	rows, err := s.store.ListLinearWebhookEvents(ctx, orgID, limit)
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
