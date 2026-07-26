package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

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
// synced message into a Linear comment reaction. When the Slack user has a
// linked Linear identity, the Linear reaction is authored as that user.
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
		// Idempotency: already mirrored this Slack user's reaction.
		if _, err := e.store.MirroredReactionByCounterpart(ctx, orgID, sourceSlack, parentID, ev.Reaction, ev.User); err == nil {
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("slack reaction %s: counterpart lookup: %w", eventID, err)
		}
		res, err := e.intg.LinearCreateReaction(ctx, orgID, link.LinearCommentID, unicode, ev.User)
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
			return nil // nothing we mirrored (or never synced this emoji)
		}
		if err != nil {
			return fmt.Errorf("slack reaction %s: counterpart lookup: %w", eventID, err)
		}
		if err := e.intg.LinearDeleteReaction(ctx, orgID, row.EventSourceID, row.ActingUserID); err != nil {
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

// linearReactionFromApp reports whether a Linear reaction webhook was caused by
// an OAuth application (our own reactionCreate echoes). Linear often labels
// these actor.type=user with an @oauthapp.linear.app email.
func linearReactionFromApp(a linearActor) bool {
	if a.Type != "" && a.Type != "user" {
		return true
	}
	email := strings.ToLower(strings.TrimSpace(a.Email))
	return strings.HasSuffix(email, "@oauthapp.linear.app")
}

// onLinearReaction mirrors a human Linear comment reaction into Slack.
// Defense 1 for app-authored reactions is linearReactionFromApp in
// OnLinearEvent before this is called. When the Linear user has a linked Slack
// user token, the Slack reaction is attributed to that person; if that token
// can't react (missing scope / not in channel), we fall back to the bot.
func (e *Engine) onLinearReaction(ctx context.Context, orgID string, p linearPayload) error {
	r := p.Linear.Reaction
	if r == nil || r.CommentID == "" || r.Emoji == "" || r.ID == "" {
		return nil
	}

	link, err := e.store.LinkByLinearComment(ctx, orgID, r.CommentID)
	if errors.Is(err, store.ErrNotFound) {
		slog.InfoContext(ctx, "sync: linear reaction: skip unmapped comment",
			"org_id", orgID, "reaction_id", r.ID, "comment_id", r.CommentID)
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
			return nil // redelivery
		} else if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("linear reaction %s: source lookup: %w", r.ID, err)
		}

		token, actingUserID, slackActorID, err := e.slackReactionToken(ctx, orgID, r.UserID)
		if err != nil {
			return fmt.Errorf("linear reaction %s: slack token: %w", r.ID, err)
		}
		if err := e.slack.AddReaction(ctx, token, link.SlackChannelID, link.SlackTS, shortcode); err != nil {
			if actingUserID == "" {
				return fmt.Errorf("linear reaction %s: slack add: %w", r.ID, err)
			}
			// User token often lacks reactions:write (pre-reconnect) or the
			// person isn't in the issue channel — fall back to the bot.
			slog.InfoContext(ctx, "sync: linear reaction: user token failed, falling back to bot",
				"org_id", orgID, "reaction_id", r.ID, "error", err)
			botToken, berr := e.intg.SlackBotToken(ctx, orgID)
			if berr != nil {
				return fmt.Errorf("linear reaction %s: slack add (user): %w; bot token: %v", r.ID, err, berr)
			}
			if err := e.slack.AddReaction(ctx, botToken, link.SlackChannelID, link.SlackTS, shortcode); err != nil {
				return fmt.Errorf("linear reaction %s: slack add: %w", r.ID, err)
			}
			actingUserID, slackActorID = "", ""
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
		shortcode := ""
		actingUserID := ""
		if err == nil {
			shortcode = row.CounterpartEmoji
			actingUserID = row.ActingUserID
		} else if errors.Is(err, store.ErrNotFound) {
			// Row missing (e.g. never recorded): best-effort map from webhook emoji
			// and resolve token from the Linear reactor when possible.
			mapped, ok := emojiToSlack(r.Emoji)
			if !ok {
				return nil
			}
			shortcode = mapped
			if _, uid, _, rerr := e.slackReactionToken(ctx, orgID, r.UserID); rerr == nil {
				actingUserID = uid
			}
		} else {
			return fmt.Errorf("linear reaction %s: source lookup: %w", r.ID, err)
		}

		token, err := e.slackTokenForActor(ctx, orgID, actingUserID)
		if err != nil {
			return fmt.Errorf("linear reaction %s: slack token: %w", r.ID, err)
		}
		if err := e.slack.RemoveReaction(ctx, token, link.SlackChannelID, link.SlackTS, shortcode); err != nil {
			if actingUserID == "" {
				return fmt.Errorf("linear reaction %s: slack remove: %w", r.ID, err)
			}
			slog.InfoContext(ctx, "sync: linear reaction: user token remove failed, falling back to bot",
				"org_id", orgID, "reaction_id", r.ID, "error", err)
			botToken, berr := e.intg.SlackBotToken(ctx, orgID)
			if berr != nil {
				return fmt.Errorf("linear reaction %s: slack remove (user): %w; bot token: %v", r.ID, err, berr)
			}
			if err := e.slack.RemoveReaction(ctx, botToken, link.SlackChannelID, link.SlackTS, shortcode); err != nil {
				return fmt.Errorf("linear reaction %s: slack remove: %w", r.ID, err)
			}
		}
		if err := e.store.DeleteMirroredReaction(ctx, orgID, sourceLinear, r.ID); err != nil {
			slog.ErrorContext(ctx, "sync: linear reaction: delete link failed",
				"org_id", orgID, "reaction_id", r.ID, "error", err)
		}
		return nil
	}
	return nil
}

// slackReactionToken resolves a Slack token for a Linear reactor: user token
// when linked, else bot. Returns acting NotifBuddy user id and Slack U… when
// using a user token (empty actor id means bot).
func (e *Engine) slackReactionToken(ctx context.Context, orgID, linearUserID string) (token, actingUserID, slackActorID string, err error) {
	if linearUserID != "" {
		uid, rerr := e.intg.ResolveUserIDByLinearUserID(ctx, orgID, linearUserID)
		switch {
		case rerr == nil:
			t, terr := e.intg.SlackUserToken(ctx, orgID, uid)
			switch {
			case terr == nil:
				sid, _ := e.intg.SlackUserIDByUserID(ctx, orgID, uid)
				return t, uid, sid, nil
			case !errors.Is(terr, store.ErrNotFound):
				return "", "", "", terr
			}
		case !errors.Is(rerr, store.ErrNotFound):
			return "", "", "", rerr
		}
	}
	t, err := e.intg.SlackBotToken(ctx, orgID)
	if err != nil {
		return "", "", "", err
	}
	return t, "", "", nil
}

// slackTokenForActor returns the Slack token used when the mirrored row was
// written: user token for actingUserID when set, else bot.
func (e *Engine) slackTokenForActor(ctx context.Context, orgID, actingUserID string) (string, error) {
	if actingUserID != "" {
		t, err := e.intg.SlackUserToken(ctx, orgID, actingUserID)
		switch {
		case err == nil:
			return t, nil
		case !errors.Is(err, store.ErrNotFound):
			return "", err
		}
	}
	return e.intg.SlackBotToken(ctx, orgID)
}
