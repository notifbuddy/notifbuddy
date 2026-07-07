package sync

import (
	"context"
	"encoding/json"
	"errors"
	"log"

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

// slackPayload is the subset of the stored Slack event_callback body we read.
type slackPayload struct {
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
}

// OnSlackEvent is the subscriber for integrations.slack.webhook_event. It
// mirrors a human Slack message in a synced channel into a Linear comment on the
// mapped issue.
func (e *Engine) OnSlackEvent(ctx context.Context, msg pubsub.Message) {
	var ref slackEventRef
	if err := json.Unmarshal(msg.Payload, &ref); err != nil {
		log.Printf("sync: slack event: bad ref: %v", err)
		return
	}
	if ref.OrgID == "" {
		return
	}
	if e.orgLocked(ctx, ref.OrgID) {
		log.Printf("sync: slack event %s: org %s locked (billing); dropped", ref.EventID, ref.OrgID)
		return
	}

	raw, err := e.store.SlackWebhookPayload(ctx, ref.EventID)
	if err != nil {
		log.Printf("sync: slack event %s: load payload: %v", ref.EventID, err)
		return
	}
	var p slackPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		log.Printf("sync: slack event %s: parse payload: %v", ref.EventID, err)
		return
	}
	ev := p.Event

	// Only real user messages mirror.
	if ev.Type != "message" {
		return
	}
	// Defense 1: drop the bot's own messages. Our mirrored posts are authored by
	// the bot (bot_id set); message subtypes like bot_message / message_changed
	// are also not human posts. This is what stops the Linear->Slack->Linear echo.
	if ev.BotID != "" || ev.Subtype != "" || ev.User == "" {
		return
	}

	token, err := e.intg.SlackBotToken(ctx, ref.OrgID)
	if err != nil {
		log.Printf("sync: slack event: slack token: %v", err)
		return
	}
	// Belt check: if we can resolve our own bot user id and it authored this,
	// drop it. (Covers the rare case a mirrored post lacks bot_id.)
	if botID, err := e.slack.AuthTestUserID(ctx, token); err == nil && botID != "" && botID == ev.User {
		return
	}

	// Route: which Linear issue owns this channel?
	issueID, err := e.store.IssueForChannel(ctx, ref.OrgID, ev.Channel)
	if errors.Is(err, store.ErrNotFound) {
		return // message in a channel we don't sync
	}
	if err != nil {
		log.Printf("sync: slack event: issue lookup: %v", err)
		return
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

	// Attribution: show the Slack author's name/avatar on the Linear comment.
	username, iconURL := e.slackAuthor(ctx, token, ev.User)

	comment, err := e.intg.LinearCreateComment(ctx, ref.OrgID, integrations.LinearCreateCommentInput{
		IssueID:        issueID,
		Body:           ev.Text,
		ParentID:       parentComment,
		CreateAsUser:   username,
		DisplayIconURL: iconURL,
	})
	if err != nil {
		log.Printf("sync: slack event: create linear comment: %v", err)
		return
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
		log.Printf("sync: slack event: record link: %v", err)
	}

	e.fireMessage(ctx, ref.OrgID, TopicLinearComment, MessageEvent{
		OrgID:           ref.OrgID,
		Direction:       "slack->linear",
		LinearIssueID:   issueID,
		LinearCommentID: comment.ID,
		SlackChannel:    ev.Channel,
		SlackTS:         ev.TS,
	})
}

// slackAuthor resolves a Slack user id to a display name + avatar for
// attribution on the Linear side. Best-effort: returns ("", "") on failure,
// which Linear renders as the app itself.
func (e *Engine) slackAuthor(ctx context.Context, token, userID string) (name, iconURL string) {
	u, err := e.slack.UserByID(ctx, token, userID)
	if err != nil {
		log.Printf("sync: slack author lookup %s: %v", userID, err)
		return "", ""
	}
	return u.Name, u.IconURL
}
