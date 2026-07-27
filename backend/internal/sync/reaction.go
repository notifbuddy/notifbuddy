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
// synced message into a Linear comment reaction. Requires a linked Linear user
// token; otherwise the event is dropped (NOT-66).
func (e *Engine) onSlackReaction(ctx context.Context, orgID, eventID, ownBotID string, ev slackEventBody) error {
	if ev.User == "" || (ownBotID != "" && ownBotID == ev.User) {
		return nil // Defense 1: drop our own bot reactions
	}
	if ev.Item.Type != "message" || ev.Item.Channel == "" || ev.Item.TS == "" || ev.Reaction == "" {
		return nil
	}

	link, err := e.store.LinkBySlackTS(ctx, orgID, ev.Item.Channel, ev.Item.TS)
	if errors.Is(err, store.ErrNotFound) {
		return nil
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
		if _, err := e.store.MirroredReactionByCounterpart(ctx, orgID, sourceSlack, parentID, ev.Reaction, ev.User); err == nil {
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("slack reaction %s: counterpart lookup: %w", eventID, err)
		}
		res, err := e.intg.LinearCreateReaction(ctx, orgID, link.LinearCommentID, unicode, ev.User)
		if errors.Is(err, store.ErrNotFound) {
			slog.InfoContext(ctx, "sync: slack reaction: skip unlinked user",
				"event_id", eventID, "org_id", orgID, "slack_user", ev.User)
			return nil
		}
		if err != nil {
			return fmt.Errorf("slack reaction %s: linear create: %w", eventID, err)
		}
		if err := e.store.RecordMirroredReaction(ctx, orgID, store.MirroredReaction{
			EventSource:         sourceLinear,
			EventSourceID:       res.ID,
			ParentSource:        sourceLinear,
			ParentSourceID:      link.LinearCommentID,
			Emoji:               unicode,
			CounterpartSource:   sourceSlack,
			CounterpartParentID: parentID,
			CounterpartEmoji:    ev.Reaction,
			CounterpartActorID:  ev.User,
			ActingUserID:        res.ActingUserID,
		}); err != nil {
			slog.ErrorContext(ctx, "sync: slack reaction: record link failed",
				"event_id", eventID, "org_id", orgID, "error", err)
		}
		return nil

	case "reaction_removed":
		row, err := e.store.MirroredReactionByCounterpart(ctx, orgID, sourceSlack, parentID, ev.Reaction, ev.User)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("slack reaction %s: counterpart lookup: %w", eventID, err)
		}
		if err := e.intg.LinearDeleteReaction(ctx, orgID, row.EventSourceID, row.ActingUserID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				slog.InfoContext(ctx, "sync: slack reaction: skip unlinked user",
					"event_id", eventID, "org_id", orgID, "slack_user", ev.User)
				return nil
			}
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
// Requires a linked Slack user token; otherwise the event is dropped (NOT-66).
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
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("linear reaction %s: source lookup: %w", r.ID, err)
		}

		token, actingUserID, slackActorID, err := e.slackReactionToken(ctx, orgID, r.UserID)
		if errors.Is(err, store.ErrNotFound) {
			slog.InfoContext(ctx, "sync: linear reaction: skip unlinked user",
				"org_id", orgID, "reaction_id", r.ID, "linear_user", r.UserID)
			return nil
		}
		if err != nil {
			return fmt.Errorf("linear reaction %s: slack token: %w", r.ID, err)
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
			CounterpartActorID:  slackActorID,
			ActingUserID:        actingUserID,
		}); err != nil {
			slog.ErrorContext(ctx, "sync: linear reaction: record link failed",
				"org_id", orgID, "reaction_id", r.ID, "error", err)
		}
		return nil

	case "remove":
		row, err := e.store.MirroredReactionBySource(ctx, orgID, sourceLinear, r.ID)
		var shortcode, token string
		switch {
		case err == nil:
			shortcode = row.CounterpartEmoji
			token, err = e.intg.SlackUserToken(ctx, orgID, row.ActingUserID)
		case errors.Is(err, store.ErrNotFound):
			var ok bool
			shortcode, ok = emojiToSlack(r.Emoji)
			if !ok {
				return nil
			}
			token, _, _, err = e.slackReactionToken(ctx, orgID, r.UserID)
		default:
			return fmt.Errorf("linear reaction %s: source lookup: %w", r.ID, err)
		}
		if errors.Is(err, store.ErrNotFound) {
			slog.InfoContext(ctx, "sync: linear reaction: skip unlinked user",
				"org_id", orgID, "reaction_id", r.ID, "linear_user", r.UserID)
			return nil
		}
		if err != nil {
			return fmt.Errorf("linear reaction %s: slack token: %w", r.ID, err)
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

// slackReactionToken resolves Slack user token + U… for a Linear reactor.
// store.ErrNotFound when unlinked (NOT-66).
func (e *Engine) slackReactionToken(ctx context.Context, orgID, linearUserID string) (token, actingUserID, slackActorID string, err error) {
	if linearUserID == "" {
		return "", "", "", store.ErrNotFound
	}
	uid, err := e.intg.ResolveUserIDByLinearUserID(ctx, orgID, linearUserID)
	if err != nil {
		return "", "", "", err
	}
	t, err := e.intg.SlackUserToken(ctx, orgID, uid)
	if err != nil {
		return "", "", "", err
	}
	sid, err := e.intg.SlackUserIDByUserID(ctx, orgID, uid)
	if err != nil {
		return "", "", "", err
	}
	return t, uid, sid, nil
}
