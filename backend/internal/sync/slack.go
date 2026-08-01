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

// slackEventBody is the subset of Slack's event object we act on (messages,
// app_mention, and reactions).
type slackEventBody struct {
	Type     string      `json:"type"`
	Subtype  string      `json:"subtype"`
	User     string      `json:"user"`
	BotID    string      `json:"bot_id"`
	Text     string      `json:"text"`
	Channel  string      `json:"channel"`
	TS       string      `json:"ts"`
	ThreadTS string      `json:"thread_ts"`
	Reaction string      `json:"reaction"`
	Item     slackItem   `json:"item"`
	Files    []slackFile `json:"files"`
}

// slackItem is the target of a reaction_added / reaction_removed event.
type slackItem struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

// slackPayload is the stored event envelope: the writer
// (integrations.WriteSlackWebhook) wraps Slack's raw event_callback body under
// `slack` with a top-level `event_source`, mirroring the Linear envelope. We
// read the subset of the inner body we act on.
type slackPayload struct {
	EventSource string `json:"event_source"`
	Slack       struct {
		Event slackEventBody `json:"event"`
	} `json:"slack"`
}

// slackFile is the subset of a Slack file object we mirror. URLPrivate (and
// its download variant) only serve with the workspace bot token.
type slackFile struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Mimetype           string `json:"mimetype"`
	Size               int64  `json:"size"`
	URLPrivate         string `json:"url_private"`
	URLPrivateDownload string `json:"url_private_download"`
}

// OnSlackEvent is the subscriber for integrations.slack.webhook_event. It
// handles @bot create/close commands, or mirrors a human Slack message in a
// synced channel into a Linear comment on the mapped issue. A returned error
// nacks the message for redelivery; errors after the Linear comment exists are
// only logged so a retry can't double-post.
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
	if e.orgSyncDisabled(ctx, ref.OrgID) {
		slog.InfoContext(ctx, "sync: slack event dropped: sync disabled", "event_id", ref.EventID, "org_id", ref.OrgID)
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

	token, err := e.intg.SlackBotToken(ctx, ref.OrgID)
	if err != nil {
		return fmt.Errorf("slack event %s: slack token: %w", ref.EventID, err)
	}
	bot, botOK := e.resolveBotIdentity(ctx, ref.OrgID, token)
	// Belt check: drop our own posts. Prefer the resolved identity; if users.info
	// failed, fall back to auth.test alone so loop prevention still works.
	ownBotID := ""
	if botOK {
		ownBotID = bot.SlackUserID
	} else if id, err := e.slack.AuthTestUserID(ctx, token); err == nil {
		ownBotID = id
	}

	// Reactions are handled before the message path: they nest channel/ts under
	// item and must not fall through to IssueForChannel(ev.Channel).
	switch ev.Type {
	case "reaction_added", "reaction_removed":
		return e.onSlackReaction(ctx, ref.OrgID, ref.EventID, ownBotID, ev)
	case "message":
		// Defense 1: drop the bot's own messages. Our mirrored posts are authored by
		// the bot (bot_id set); message subtypes like bot_message / message_changed
		// are also not human posts. This is what stops the Linear->Slack->Linear echo.
		// file_share is the one subtype a human post can carry (a message with an
		// attachment), so it passes through — the bot-identity checks (bot_id here,
		// AuthTestUserID below) still drop the bot's own file shares.
		if ev.BotID != "" || (ev.Subtype != "" && ev.Subtype != "file_share") || ev.User == "" {
			return nil
		}
	case "app_mention":
		if ev.User == "" {
			return nil
		}
	default:
		return nil
	}

	if ownBotID != "" && ownBotID == ev.User {
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

	// @bot command? Classify before mirroring; a create/close short-circuits.
	if botOK && e.classifier != nil && botMentioned(ev.Text, bot.SlackUserID, bot.SlackDisplayName) {
		if e.runNotifBuddyCommand(ctx, ref.OrgID, issueID, ev.Text, ev.User, nil) {
			return nil
		}
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

	var attachments []integrations.LinearCommentAttachment
	body := e.rewriteSlackMentions(ctx, ref.OrgID, token, ev.Text)
	for _, f := range ev.Files {
		fileURL := firstNonEmpty(f.URLPrivateDownload, f.URLPrivate)
		if fileURL == "" {
			continue
		}
		data, err := e.slack.DownloadFile(ctx, token, fileURL)
		if err != nil {
			slog.ErrorContext(ctx, "sync: slack event: file download failed",
				"event_id", ref.EventID, "org_id", ref.OrgID, "file_id", f.ID, "error", err)
			body += fmt.Sprintf("\n\n_(attachment %q could not be synced)_", f.Name)
			continue
		}
		attachments = append(attachments, integrations.LinearCommentAttachment{
			Filename:    f.Name,
			ContentType: f.Mimetype,
			Data:        data,
		})
	}

	comment, err := e.intg.LinearCreateComment(ctx, ref.OrgID, integrations.LinearCreateCommentInput{
		IssueID:       issueID,
		Body:          body,
		ParentID:      parentComment,
		SlackAuthorID: ev.User,
		Attachments:   attachments,
	})
	if errors.Is(err, store.ErrNotFound) {
		slog.InfoContext(ctx, "sync: slack message: skip unlinked user",
			"event_id", ref.EventID, "org_id", ref.OrgID, "slack_user", ev.User)
		return nil
	}
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
