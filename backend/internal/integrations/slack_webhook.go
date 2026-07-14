package integrations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/store"
)

// SlackWebhookReceivedTopic carries each verified raw Slack event_callback
// from the HTTP receiver to the writer consumer (payload = raw event body).
const SlackWebhookReceivedTopic = "integrations.slack.webhook.received"

// SlackWebhookTopic is the processed topic the writer fires once an event is
// persisted; subscribers (the sync engine) re-read the stored payload.
const SlackWebhookTopic = "integrations.slack.webhook_event"

// slackMaxTimestampSkew is how stale a Slack request timestamp may be before we
// reject it as a possible replay. Slack recommends 5 minutes.
const slackMaxTimestampSkew = 5 * time.Minute

// slackWebhookEvent is the published event shape (also what subscribers see).
// It carries just enough to route: the sync engine reads the stored payload for
// the full body.
type slackWebhookEvent struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	TeamID    string `json:"team_id,omitempty"`
	OrgID     string `json:"org_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
}

// HandleSlackWebhook receives a Slack Events API delivery. It handles three
// cases in order:
//
//  1. url_verification — Slack's one-time challenge handshake when you set the
//     Request URL; echo the challenge back in plain text.
//  2. a signed event_callback — verify X-Slack-Signature, then, like the Linear
//     receiver, STORE the event durably and PUBLISH a best-effort notification.
//
// Signature verification is over the RAW body, so we read it once and verify
// before parsing.
func (s *Service) HandleSlackWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		http.Error(w, "integrations not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	// Verify the signature before doing anything else with the body. Fail closed:
	// with no signing secret we cannot authenticate the request, so refuse it
	// rather than accepting an unsigned (forgeable) webhook — an unset secret
	// would otherwise let anyone POST events attributed to any workspace. This
	// mirrors the Stripe/WorkOS webhook handlers.
	secret := s.cfg.Slack.SigningSecret
	if secret == "" {
		slog.ErrorContext(r.Context(), "integrations: slack signing secret not configured; refusing webhook")
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}
	if !validSlackSignature(secret, body, r.Header.Get("X-Slack-Request-Timestamp"),
		r.Header.Get("X-Slack-Signature"), time.Now()) {
		slog.WarnContext(r.Context(), "integrations: slack webhook signature mismatch")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Peek at the envelope: type distinguishes url_verification from
	// event_callback; the rest is present only on event_callback.
	var env struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		TeamID    string `json:"team_id"`
		EventID   string `json:"event_id"`
		Event     struct {
			Type    string `json:"type"`
			Channel string `json:"channel"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// 1. url_verification handshake — echo the challenge, nothing to store.
	if env.Type == "url_verification" {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(env.Challenge))
		return
	}

	// Only event_callback deliveries carry an event to process.
	if env.Type != "event_callback" || env.EventID == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// PUBLISH the raw event durably; the writer consumer persists it (dedup on
	// Slack's event id) and fires the processed topic. A failed publish means
	// the event is not recorded anywhere, so surface a 5xx for Slack to retry.
	// Slack expects a response within 3 seconds; a publish is one INSERT.
	if err := s.pub.Publish(r.Context(), pubsub.Message{
		Topic:   SlackWebhookReceivedTopic,
		Payload: body,
		Attributes: map[string]string{
			"event_id":   env.EventID,
			"event_type": env.Event.Type,
			"team_id":    env.TeamID,
			"channel_id": env.Event.Channel,
		},
	}); err != nil {
		slog.ErrorContext(r.Context(), "integrations: publish slack webhook", "event_id", env.EventID, "error", err)
		http.Error(w, "failed to accept event", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// WriteSlackWebhook consumes integrations.slack.webhook.received: it resolves
// the owning org, persists the event (idempotent on Slack's event id), and
// publishes the routing envelope on the processed topic. A returned error
// nacks the message for redelivery — including when the insert committed but
// the envelope publish failed, which the envelope_published flag turns into a
// publish retry instead of a lost event.
func (s *Service) WriteSlackWebhook(ctx context.Context, msg pubsub.Message) error {
	evt := slackWebhookEvent{
		EventID:   msg.Attributes["event_id"],
		EventType: msg.Attributes["event_type"],
		TeamID:    msg.Attributes["team_id"],
		ChannelID: msg.Attributes["channel_id"],
	}

	// Resolve which org owns this workspace (best-effort).
	if evt.TeamID != "" {
		if id, err := s.store.OrgIDBySlackTeam(ctx, evt.TeamID); err == nil {
			evt.OrgID = id
		}
	}

	inserted, published, err := s.store.InsertSlackWebhookEvent(ctx, store.SlackWebhookEvent{
		EventID:   evt.EventID,
		EventType: evt.EventType,
		TeamID:    evt.TeamID,
		OrgID:     evt.OrgID,
		ChannelID: evt.ChannelID,
		Payload:   json.RawMessage(msg.Payload),
	})
	if err != nil {
		return fmt.Errorf("store slack webhook %s: %w", evt.EventID, err)
	}
	if !inserted && published {
		return nil // retry of a fully-processed event: consume silently
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal slack envelope %s: %w", evt.EventID, err)
	}
	if err := s.pub.Publish(ctx, pubsub.Message{
		Topic:   SlackWebhookTopic,
		Payload: payload,
		Attributes: map[string]string{
			"event_type": evt.EventType,
			"org_id":     evt.OrgID,
		},
	}); err != nil {
		return fmt.Errorf("publish slack envelope %s: %w", evt.EventID, err)
	}
	// Failure here only risks a duplicate envelope on a later redelivery,
	// which downstream consumers must tolerate anyway (at-least-once).
	if err := s.store.MarkSlackWebhookPublished(ctx, evt.EventID); err != nil {
		slog.ErrorContext(ctx, "integrations: mark slack webhook published", "event_id", evt.EventID, "error", err)
	}
	return nil
}

// validSlackSignature verifies the X-Slack-Signature header. Slack computes
// "v0=" + hex(HMAC-SHA256(signingSecret, "v0:"+timestamp+":"+rawBody)). We also
// reject stale timestamps (replay protection). now is injected for tests.
func validSlackSignature(secret string, body []byte, timestamp, signature string, now time.Time) bool {
	if timestamp == "" || signature == "" {
		return false
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if delta := now.Sub(time.Unix(ts, 0)); delta > slackMaxTimestampSkew || delta < -slackMaxTimestampSkew {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "v0:%s:%s", timestamp, body)
	want := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(signature))
}

// ListSlackWebhooks returns an org's most recent stored Slack webhook events.
func (s *Service) ListSlackWebhooks(ctx context.Context, orgID string, limit int) ([]WebhookEvent, error) {
	if !s.Enabled() || orgID == "" {
		return nil, nil
	}
	rows, err := s.store.ListSlackWebhookEvents(ctx, orgID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]WebhookEvent, 0, len(rows))
	for _, e := range rows {
		out = append(out, WebhookEvent{
			DeliveryID: e.EventID,
			EventType:  e.EventType,
			ReceivedAt: e.ReceivedAt,
			Payload:    e.Payload,
		})
	}
	return out, nil
}
