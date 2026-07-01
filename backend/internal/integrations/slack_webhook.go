package integrations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/store"
)

// SlackWebhookTopic is the logical topic fired for each inbound Slack event we
// receive and store. Backends (memory/SNS) map it to a concrete destination.
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

	// Verify the signature before doing anything else with the body. If a signing
	// secret is configured, it must match (and the timestamp must be fresh).
	if secret := s.cfg.Slack.SigningSecret; secret != "" {
		if !validSlackSignature(secret, body, r.Header.Get("X-Slack-Request-Timestamp"),
			r.Header.Get("X-Slack-Signature"), time.Now()) {
			log.Printf("integrations: slack webhook signature mismatch")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
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

	// Resolve which org owns this workspace (best-effort).
	orgID := ""
	if env.TeamID != "" {
		if id, err := s.store.OrgIDBySlackTeam(r.Context(), env.TeamID); err == nil {
			orgID = id
		}
	}

	// 2a. STORE — durable source of truth. Idempotent on Slack's event id.
	inserted, err := s.store.InsertSlackWebhookEvent(r.Context(), store.SlackWebhookEvent{
		EventID:   env.EventID,
		EventType: env.Event.Type,
		TeamID:    env.TeamID,
		OrgID:     orgID,
		ChannelID: env.Event.Channel,
		Payload:   json.RawMessage(body),
	})
	if err != nil {
		log.Printf("integrations: store slack webhook %s: %v", env.EventID, err)
		http.Error(w, "failed to store event", http.StatusInternalServerError)
		return
	}
	if !inserted {
		// Retry of an already-stored event: ack without re-publishing.
		w.WriteHeader(http.StatusOK)
		return
	}

	// 2b. PUBLISH — separate, best-effort notification.
	s.publishSlackWebhook(r, slackWebhookEvent{
		EventID:   env.EventID,
		EventType: env.Event.Type,
		TeamID:    env.TeamID,
		OrgID:     orgID,
		ChannelID: env.Event.Channel,
	})

	// Slack expects a 200 within 3 seconds; publishing is async of that.
	w.WriteHeader(http.StatusOK)
}

// publishSlackWebhook fires integrations.slack.webhook_event. Failures are
// logged, not surfaced — the event is already stored.
func (s *Service) publishSlackWebhook(r *http.Request, evt slackWebhookEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		log.Printf("integrations: marshal slack webhook event %s: %v", evt.EventID, err)
		return
	}
	msg := pubsub.Message{
		Topic:   SlackWebhookTopic,
		Payload: payload,
		Attributes: map[string]string{
			"event_type": evt.EventType,
			"org_id":     evt.OrgID,
		},
	}
	if err := s.pub.Publish(r.Context(), msg); err != nil {
		log.Printf("integrations: publish %s for event %s failed (event stored): %v",
			SlackWebhookTopic, evt.EventID, err)
	}
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
