package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"xolo/backend/internal/store"
)

// sourceSlack is the event_source / counterpart_source value for Slack, in the
// same vocabulary as webhook envelopes and mirrored_assets.
const sourceSlack = "slack"

// slackParentID encodes a Slack message identity for mirrored_reactions.counterpart_parent_id.
func slackParentID(channelID, ts string) string {
	return channelID + ":" + ts
}

// onSlackReaction mirrors a human reaction_added / reaction_removed on a
// synced message into a Linear comment reaction.
func (e *Engine) onSlackReaction(ctx context.Context, orgID, eventID, ownBotID string, ev slackEventBody) error {
	if ev.User == "" || (ownBotID != "" && ownBotID == ev.User) {
		return nil // Defense 1: drop our own bot reactions
	}
	if ev.Item.Type != "message" || ev.Item.Channel == "" || ev.Item.TS == "" || ev.Reaction == "" {
		return nil
	}

	link, err := e.store.LinkBySlackTS(ctx, orgID, ev.Item.Channel, ev.Item.TS)
	if errors.Is(err, store.ErrNotFound) {
		return nil // reaction on a message we don't mirror
	}
	if err != nil {
		return fmt.Errorf("slack reaction %s: mirror lookup: %w", eventID, err)
	}

	parentID := slackParentID(ev.Item.Channel, ev.Item.TS)

	switch ev.Type {
	case "reaction_added":
		unicode, ok := emojiToLinear(ev.Reaction)
		if !ok {
			slog.InfoContext(ctx, "sync: slack reaction: skip unmapped emoji",
				"event_id", eventID, "org_id", orgID, "emoji", ev.Reaction)
			return nil
		}
		// Idempotency: already mirrored this Slack reaction.
		if _, err := e.store.MirroredReactionByCounterpart(ctx, orgID, sourceSlack, parentID, ev.Reaction); err == nil {
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("slack reaction %s: counterpart lookup: %w", eventID, err)
		}
		reactionID, err := e.intg.LinearCreateReaction(ctx, orgID, link.LinearCommentID, unicode)
		if err != nil {
			return fmt.Errorf("slack reaction %s: linear create: %w", eventID, err)
		}
		if err := e.store.RecordMirroredReaction(ctx, orgID, store.MirroredReaction{
			EventSource:         sourceLinear,
			EventSourceID:       reactionID,
			ParentSource:        sourceLinear,
			ParentSourceID:      link.LinearCommentID,
			Emoji:               unicode,
			CounterpartSource:   sourceSlack,
			CounterpartParentID: parentID,
			CounterpartEmoji:    ev.Reaction,
		}); err != nil {
			slog.ErrorContext(ctx, "sync: slack reaction: record link failed",
				"event_id", eventID, "org_id", orgID, "error", err)
		}
		return nil

	case "reaction_removed":
		row, err := e.store.MirroredReactionByCounterpart(ctx, orgID, sourceSlack, parentID, ev.Reaction)
		if errors.Is(err, store.ErrNotFound) {
			return nil // nothing we mirrored (or never synced this emoji)
		}
		if err != nil {
			return fmt.Errorf("slack reaction %s: counterpart lookup: %w", eventID, err)
		}
		if err := e.intg.LinearDeleteReaction(ctx, orgID, row.EventSourceID); err != nil {
			return fmt.Errorf("slack reaction %s: linear delete: %w", eventID, err)
		}
		if err := e.store.DeleteMirroredReaction(ctx, orgID, row.EventSource, row.EventSourceID); err != nil {
			slog.ErrorContext(ctx, "sync: slack reaction: delete link failed",
				"event_id", eventID, "org_id", orgID, "error", err)
		}
		return nil
	}
	return nil
}

// onLinearReaction mirrors a human Linear comment reaction into Slack.
// Defense 1 for app-authored reactions is the actor.type != "user" check in
// OnLinearEvent before this is called.
func (e *Engine) onLinearReaction(ctx context.Context, orgID string, p linearPayload) error {
	r := p.Linear.Reaction
	if r == nil || r.CommentID == "" || r.Emoji == "" || r.ID == "" {
		return nil
	}

	link, err := e.store.LinkByLinearComment(ctx, orgID, r.CommentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("linear reaction %s: mirror lookup: %w", r.ID, err)
	}

	token, err := e.intg.SlackBotToken(ctx, orgID)
	if err != nil {
		return fmt.Errorf("linear reaction %s: slack token: %w", r.ID, err)
	}

	parentID := slackParentID(link.SlackChannelID, link.SlackTS)

	switch p.Linear.Action {
	case "create":
		shortcode, ok := emojiToSlack(r.Emoji)
		if !ok {
			slog.InfoContext(ctx, "sync: linear reaction: skip unmapped emoji",
				"org_id", orgID, "reaction_id", r.ID, "emoji", r.Emoji)
			return nil
		}
		if _, err := e.store.MirroredReactionBySource(ctx, orgID, sourceLinear, r.ID); err == nil {
			return nil // redelivery
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("linear reaction %s: source lookup: %w", r.ID, err)
		}
		if err := e.slack.AddReaction(ctx, token, link.SlackChannelID, link.SlackTS, shortcode); err != nil {
			return fmt.Errorf("linear reaction %s: slack add: %w", r.ID, err)
		}
		if err := e.store.RecordMirroredReaction(ctx, orgID, store.MirroredReaction{
			EventSource:         sourceLinear,
			EventSourceID:       r.ID,
			ParentSource:        sourceLinear,
			ParentSourceID:      r.CommentID,
			Emoji:               r.Emoji,
			CounterpartSource:   sourceSlack,
			CounterpartParentID: parentID,
			CounterpartEmoji:    shortcode,
		}); err != nil {
			slog.ErrorContext(ctx, "sync: linear reaction: record link failed",
				"org_id", orgID, "reaction_id", r.ID, "error", err)
		}
		return nil

	case "remove":
		row, err := e.store.MirroredReactionBySource(ctx, orgID, sourceLinear, r.ID)
		shortcode := ""
		if err == nil {
			shortcode = row.CounterpartEmoji
		} else if errors.Is(err, store.ErrNotFound) {
			// Row missing (e.g. never recorded): best-effort map from webhook emoji.
			if mapped, ok := emojiToSlack(r.Emoji); ok {
				shortcode = mapped
			} else {
				return nil
			}
		} else {
			return fmt.Errorf("linear reaction %s: source lookup: %w", r.ID, err)
		}
		if err := e.slack.RemoveReaction(ctx, token, link.SlackChannelID, link.SlackTS, shortcode); err != nil {
			return fmt.Errorf("linear reaction %s: slack remove: %w", r.ID, err)
		}
		if err := e.store.DeleteMirroredReaction(ctx, orgID, sourceLinear, r.ID); err != nil {
			slog.ErrorContext(ctx, "sync: linear reaction: delete link failed",
				"org_id", orgID, "reaction_id", r.ID, "error", err)
		}
		return nil
	}
	return nil
}
