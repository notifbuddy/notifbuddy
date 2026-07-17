package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"xolo/backend/internal/integrations"
	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/store"
)

// slackEventRef is the routing envelope published on the Slack ingestion topic.
type slackEventRef struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	OrgID     string `json:"org_id"`
	ChannelID string `json:"channel_id"`
}

// slackPayload is the stored event envelope: the writer
// (integrations.WriteSlackWebhook) wraps Slack's raw event_callback body under
// `slack` with a top-level `event_source`, mirroring the Linear envelope. We
// read the subset of the inner body we act on.
type slackPayload struct {
	EventSource string `json:"event_source"`
	Slack       struct {
		Event struct {
			Type     string `json:"type"`
			Subtype  string `json:"subtype"`
			User     string `json:"user"`
			BotID    string `json:"bot_id"`
			Text     string `json:"text"`
			Channel  string `json:"channel"`
			TS       string `json:"ts"`
			ThreadTS string `json:"thread_ts"`
		} `json:"event"`
	} `json:"slack"`
}

// OnSlackEvent is the subscriber for integrations.slack.webhook_event. It
// mirrors a human Slack message in a synced channel into a Linear comment on
// the mapped issue. A returned error nacks the message for redelivery; errors
// after the Linear comment exists are only logged so a retry can't double-post.
func (e *Engine) OnSlackEvent(ctx context.Context, msg pubsub.Message) error {
	var ref slackEventRef
	if err := json.Unmarshal(msg.Payload, &ref); err != nil {
		slog.WarnContext(ctx, "sync: slack event: bad ref", "error", err)
		return nil
	}
	if ref.OrgID == "" {
		return nil
	}
	if e.orgLocked(ctx, ref.OrgID) {
		slog.InfoContext(ctx, "sync: slack event dropped: org locked (billing)", "event_id", ref.EventID, "org_id", ref.OrgID)
		return nil
	}

	// The writer persisted the payload before publishing the envelope, so a
	// failure here is transient and worth a retry.
	raw, err := e.store.SlackWebhookPayload(ctx, ref.EventID)
	if err != nil {
		return fmt.Errorf("slack event %s: load payload: %w", ref.EventID, err)
	}
	var p slackPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		slog.WarnContext(ctx, "sync: slack event: parse payload failed", "event_id", ref.EventID, "error", err)
		return nil
	}
	ev := p.Slack.Event

	// Only real user messages mirror.
	if ev.Type != "message" {
		return nil
	}
	// Defense 1: drop the bot's own messages. Our mirrored posts are authored by
	// the bot (bot_id set); message subtypes like bot_message / message_changed
	// are also not human posts. This is what stops the Linear->Slack->Linear echo.
	if ev.BotID != "" || ev.Subtype != "" || ev.User == "" {
		return nil
	}

	token, err := e.intg.SlackBotToken(ctx, ref.OrgID)
	if err != nil {
		return fmt.Errorf("slack event %s: slack token: %w", ref.EventID, err)
	}
	// Belt check: if we can resolve our own bot user id and it authored this,
	// drop it. (Covers the rare case a mirrored post lacks bot_id.)
	if botID, err := e.slack.AuthTestUserID(ctx, token); err == nil && botID != "" && botID == ev.User {
		return nil
	}

	// Route: which Linear issue owns this channel?
	issueID, err := e.store.IssueForChannel(ctx, ref.OrgID, ev.Channel)
	if errors.Is(err, store.ErrNotFound) {
		return nil // message in a channel we don't sync
	}
	if err != nil {
		return fmt.Errorf("slack event %s: issue lookup: %w", ref.EventID, err)
	}

	// Idempotency: if this Slack message was already mirrored (Pub/Sub redelivers
	// a slow-but-successful message after the ack deadline), don't create a
	// second Linear comment. Each redelivery would otherwise mint a fresh comment
	// id, so the mirror link's unique key can't dedup after the fact — the check
	// must happen before the create.
	if _, err := e.store.LinkBySlackTS(ctx, ref.OrgID, ev.Channel, ev.TS); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("slack event %s: mirror lookup: %w", ref.EventID, err)
	}

	// Resolve a thread parent: a reply (thread_ts != ts) maps to the Linear
	// comment that mirrors the thread root, so the Linear comment is a reply too.
	parentComment := ""
	rootLinearComment := ""
	if ev.ThreadTS != "" && ev.ThreadTS != ev.TS {
		if link, err := e.store.LinkBySlackTS(ctx, ref.OrgID, ev.Channel, ev.ThreadTS); err == nil {
			parentComment = link.LinearCommentID
			rootLinearComment = firstNonEmpty(link.RootLinearCommentID, link.LinearCommentID)
		}
	}

	// Authorship: the service posts with the author's own linked Linear token,
	// or app-level when their identity isn't connected — never with another
	// user's credentials. The display name is best-effort provenance for the
	// app-level byline; empty just means a generic byline.
	var authorName string
	if u, err := e.slack.UserByID(ctx, token, ev.User); err == nil {
		authorName = u.Name
	}
	comment, err := e.intg.LinearCreateComment(ctx, ref.OrgID, integrations.LinearCreateCommentInput{
		IssueID:           issueID,
		Body:              ev.Text,
		ParentID:          parentComment,
		SlackAuthorID:     ev.User,
		AuthorDisplayName: authorName,
	})
	if err != nil {
		return fmt.Errorf("slack event %s: create linear comment: %w", ref.EventID, err)
	}

	if rootLinearComment == "" {
		rootLinearComment = comment.ID
	}
	if err := e.store.RecordMirroredMessage(ctx, store.MirroredMessage{
		OrgID:               ref.OrgID,
		LinearCommentID:     comment.ID,
		SlackChannelID:      ev.Channel,
		SlackTS:             ev.TS,
		RootLinearCommentID: rootLinearComment,
	}); err != nil {
		slog.ErrorContext(ctx, "sync: slack event: record link failed", "event_id", ref.EventID, "org_id", ref.OrgID, "channel_id", ev.Channel, "error", err)
	}

	e.fireMessage(ctx, ref.OrgID, TopicLinearComment, MessageEvent{
		OrgID:           ref.OrgID,
		Direction:       "slack->linear",
		LinearIssueID:   issueID,
		LinearCommentID: comment.ID,
		SlackChannel:    ev.Channel,
		SlackTS:         ev.TS,
	})
	return nil
}
