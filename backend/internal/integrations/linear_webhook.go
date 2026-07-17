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

// LinearWebhookReceivedTopic carries each verified raw Linear delivery from
// the HTTP receiver to the writer consumer (payload = raw webhook body).
const LinearWebhookReceivedTopic = "integrations.linear.webhook.received"

// LinearWebhookTopic is the processed topic the writer fires once a delivery
// is persisted; subscribers (the sync engine) re-read the stored payload.
const LinearWebhookTopic = "integrations.linear.webhook_event"

// linearWebhookEvent is the envelope published on LinearWebhookTopic.
type linearWebhookEvent struct {
	DeliveryID  string `json:"delivery_id"`
	EventType   string `json:"event_type"`
	Action      string `json:"action,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	OrgID       string `json:"org_id,omitempty"`
}

// HandleLinearWebhook receives a Linear webhook delivery. It only verifies the
// Linear-Signature HMAC and publishes the raw body durably on
// integrations.linear.webhook.received; persistence (and dedup) happens in the
// writer consumer. A publish failure returns 5xx so Linear redelivers.
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

	// 1a. Verify signature. Fail closed: with no webhook secret we cannot
	// authenticate the request, so refuse it rather than accepting an unsigned
	// (forgeable) webhook that could be attributed to any workspace. Mirrors the
	// Stripe/WorkOS webhook handlers.
	secret := s.cfg.Linear.WebhookSecret
	if secret == "" {
		slog.ErrorContext(r.Context(), "integrations: linear webhook secret not configured; refusing webhook")
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}
	if !validLinearSignature(secret, body, r.Header.Get("Linear-Signature")) {
		slog.WarnContext(r.Context(), "integrations: linear webhook signature mismatch")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
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

	// PUBLISH the raw delivery durably; the writer consumer persists it (dedup
	// on delivery id) and fires the processed topic. A failed publish means the
	// delivery is not recorded anywhere, so surface a 5xx for Linear to retry.
	if err := s.pub.Publish(r.Context(), pubsub.Message{
		Topic:   LinearWebhookReceivedTopic,
		Payload: body,
		Attributes: map[string]string{
			"delivery_id":  deliveryID,
			"event_type":   parsed.Type,
			"action":       parsed.Action,
			"workspace_id": parsed.OrganizationID,
		},
	}); err != nil {
		slog.ErrorContext(r.Context(), "integrations: publish linear webhook", "delivery_id", deliveryID, "error", err)
		http.Error(w, "failed to accept event", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// WriteLinearWebhook consumes integrations.linear.webhook.received: it
// resolves the owning org, persists the delivery (idempotent on delivery id),
// and publishes the routing envelope on the processed topic. A returned error
// nacks the message for redelivery — including when the insert committed but
// the envelope publish failed, which the envelope_published flag turns into a
// publish retry instead of a lost event.
func (s *Service) WriteLinearWebhook(ctx context.Context, msg pubsub.Message) error {
	evt := linearWebhookEvent{
		DeliveryID:  msg.Attributes["delivery_id"],
		EventType:   msg.Attributes["event_type"],
		Action:      msg.Attributes["action"],
		WorkspaceID: msg.Attributes["workspace_id"],
	}

	// Resolve which org owns this workspace (best-effort).
	if evt.WorkspaceID != "" {
		if id, err := s.store.OrgIDByLinearWorkspace(ctx, evt.WorkspaceID); err == nil {
			evt.OrgID = id
		}
	}

	// Transform the raw webhook into the stored envelope: the provider body
	// moves under `linear` with a top-level `event_source` tag. Everything
	// downstream (the sync engine, template evaluation) acts on this shape, and
	// future sources or notifbuddy-side metadata get their own top-level keys
	// without touching the provider payload.
	stored, err := json.Marshal(struct {
		EventSource string          `json:"event_source"`
		Linear      json.RawMessage `json:"linear"`
	}{EventSource: "linear", Linear: json.RawMessage(msg.Payload)})
	if err != nil {
		return fmt.Errorf("wrap linear webhook %s: %w", evt.DeliveryID, err)
	}

	inserted, published, err := s.store.InsertLinearWebhookEvent(ctx, store.LinearWebhookEvent{
		DeliveryID:  evt.DeliveryID,
		EventType:   evt.EventType,
		WorkspaceID: evt.WorkspaceID,
		OrgID:       evt.OrgID,
		Action:      evt.Action,
		Payload:     json.RawMessage(stored),
	})
	if err != nil {
		return fmt.Errorf("store linear webhook %s: %w", evt.DeliveryID, err)
	}
	if !inserted && published {
		return nil // redelivery of a fully-processed delivery: consume silently
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal linear envelope %s: %w", evt.DeliveryID, err)
	}
	if err := s.pub.Publish(ctx, pubsub.Message{
		Topic:   LinearWebhookTopic,
		Payload: payload,
		Attributes: map[string]string{
			"event_type": evt.EventType,
			"org_id":     evt.OrgID,
		},
	}); err != nil {
		return fmt.Errorf("publish linear envelope %s: %w", evt.DeliveryID, err)
	}
	// Failure here only risks a duplicate envelope on a later redelivery,
	// which downstream consumers must tolerate anyway (at-least-once).
	if err := s.store.MarkLinearWebhookPublished(ctx, evt.DeliveryID); err != nil {
		slog.ErrorContext(ctx, "integrations: mark linear webhook published", "delivery_id", evt.DeliveryID, "error", err)
	}
	return nil
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
