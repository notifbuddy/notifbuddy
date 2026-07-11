package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"xolo/backend/internal/integrations"
	"xolo/backend/internal/store"
	"xolo/backend/internal/template"
)

// ensureChannel creates the Slack channel for an issue per the org's settings
// (name template + condition), records the issue↔channel mapping, auto-adds the
// configured bots, and fires the processing topics. Caller has already checked
// that no channel exists yet. trigger is "status" or "notifbuddy".
//
// Errors before the channel is created are returned so the event can be
// redelivered and retried; once the channel exists, later failures are only
// logged — a retry would create a duplicate channel, which is worse than a
// missing mapping or invite.
func (e *Engine) ensureChannel(ctx context.Context, orgID, issueID string, settings integrations.LinearSettings, evt template.Event, trigger string) error {
	// Condition gate (if configured): must evaluate true to proceed. Eval errors
	// are deterministic (bad expression), so retrying can't help.
	if settings.ConditionExpr != "" {
		ok, err := e.tmpl.Evaluate(settings.ConditionExpr, evt)
		if err != nil {
			slog.WarnContext(ctx, "sync: ensureChannel: condition eval failed", "error", err)
			return nil
		}
		if !ok {
			return nil
		}
	}

	name, err := e.channelName(settings, evt)
	if err != nil {
		slog.WarnContext(ctx, "sync: ensureChannel: name render failed", "error", err)
		return nil
	}

	token, err := e.intg.SlackBotToken(ctx, orgID)
	if err != nil {
		return fmt.Errorf("ensureChannel: slack token: %w", err)
	}

	channelID, err := e.slack.CreateChannel(ctx, token, name)
	if err != nil {
		return fmt.Errorf("ensureChannel: create %q: %w", name, err)
	}
	if err := e.store.UpsertIssueChannel(ctx, store.IssueChannel{
		OrgID:          orgID,
		LinearIssueID:  issueID,
		SlackChannelID: channelID,
	}); err != nil {
		slog.ErrorContext(ctx, "sync: ensureChannel: record mapping failed", "org_id", orgID, "issue_id", issueID, "channel_id", channelID, "error", err)
	}

	e.fireChannel(ctx, orgID, TopicChannelCreated, ChannelEvent{
		OrgID:         orgID,
		LinearIssueID: issueID,
		SlackChannel:  channelID,
		ChannelName:   name,
		Trigger:       trigger,
	})

	// Auto-add configured members (bots + people; all Slack member ids) via a
	// single conversations.invite call.
	if len(settings.AutoAddMembers) > 0 {
		if err := e.slack.InviteUsers(ctx, token, channelID, settings.AutoAddMembers); err != nil {
			slog.ErrorContext(ctx, "sync: ensureChannel: invite members failed", "org_id", orgID, "channel_id", channelID, "error", err)
		} else {
			e.fireBots(ctx, orgID, channelID, settings.AutoAddMembers)
		}
	}
	return nil
}

// closeChannel archives the issue's channel and removes the mapping. Archiving
// (not deleting) is the default "close" per the product spec.
//
// Errors up to and including the archive call are returned for redelivery
// (archiving hasn't happened, so a retry is safe); a mapping-delete failure
// after a successful archive is only logged.
func (e *Engine) closeChannel(ctx context.Context, orgID, issueID string) error {
	channelID, err := e.store.ChannelForIssue(ctx, orgID, issueID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("closeChannel: lookup: %w", err)
	}
	token, err := e.intg.SlackBotToken(ctx, orgID)
	if err != nil {
		return fmt.Errorf("closeChannel: slack token: %w", err)
	}
	if err := e.slack.ArchiveChannel(ctx, token, channelID); err != nil {
		return fmt.Errorf("closeChannel: archive %s: %w", channelID, err)
	}
	if err := e.store.DeleteIssueChannel(ctx, orgID, issueID); err != nil {
		slog.ErrorContext(ctx, "sync: closeChannel: delete mapping failed", "org_id", orgID, "issue_id", issueID, "error", err)
	}
	e.fireChannel(ctx, orgID, TopicChannelClosed, ChannelEvent{
		OrgID:         orgID,
		LinearIssueID: issueID,
		SlackChannel:  channelID,
	})
	return nil
}

// channelName renders the settings name template, falling back to a
// deterministic name from the issue identifier, and sanitizes it to Slack's
// channel-name rules.
func (e *Engine) channelName(settings integrations.LinearSettings, evt template.Event) (string, error) {
	name := ""
	if settings.NameTemplate != "" {
		rendered, err := e.tmpl.Render(settings.NameTemplate, evt)
		if err != nil {
			return "", err
		}
		name = rendered
	}
	if strings.TrimSpace(name) == "" {
		// Fallback: tkt-<identifier> from the event data.
		if id, ok := evt.Linear["data"].(map[string]any); ok {
			if ident, ok := id["identifier"].(string); ok && ident != "" {
				name = "tkt-" + ident
			}
		}
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("empty channel name")
	}
	return sanitizeChannelName(name), nil
}

// slackChannelInvalid matches characters not allowed in a Slack channel name.
// Slack allows lowercase letters, numbers, hyphens, and underscores, max 80.
var slackChannelInvalid = regexp.MustCompile(`[^a-z0-9_-]+`)

// sanitizeChannelName lowercases, replaces invalid runs with a hyphen, trims
// stray hyphens, and caps length so CreateChannel doesn't reject it.
func sanitizeChannelName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slackChannelInvalid.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_")
	if len(s) > 80 {
		s = s[:80]
	}
	if s == "" {
		s = "channel"
	}
	return s
}

func (e *Engine) fireChannel(ctx context.Context, orgID, topic string, evt ChannelEvent) {
	b, _ := json.Marshal(evt)
	e.publish(ctx, topic, b, orgID)
}

func (e *Engine) fireBots(ctx context.Context, orgID, channelID string, bots []string) {
	b, _ := json.Marshal(BotsAddedEvent{OrgID: orgID, SlackChannel: channelID, Bots: bots})
	e.publish(ctx, TopicBotsAdded, b, orgID)
}

func (e *Engine) fireMessage(ctx context.Context, orgID, topic string, evt MessageEvent) {
	b, _ := json.Marshal(evt)
	e.publish(ctx, topic, b, orgID)
}
