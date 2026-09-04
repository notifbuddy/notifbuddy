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
	"xolo/backend/internal/slackapi"
	"xolo/backend/internal/store"
	"xolo/backend/internal/template"
)

// channelInviteExtras carries optional invite seeds for ensureChannel beyond
// the issue's creator/assignee/description mentions and AutoAddMembers.
type channelInviteExtras struct {
	// Bodies are extra markdown bodies to scan for Linear profile-URL mentions
	// (e.g. the @notifbuddy command comment).
	Bodies []string
	// Emails are extra Linear user emails to resolve via Slack lookup
	// (e.g. the comment actor on the manual Linear path).
	Emails []string
	// SlackIDs are Slack user ids to invite as-is (e.g. the Slack author on
	// the manual Slack-originated create path).
	SlackIDs []string
}

// ensureChannel creates the Slack channel for an issue per the org's settings
// (name template + condition), records the issue↔channel mapping, invites
// AutoAddMembers plus Linear issue people (creator, assignee, profile mentions),
// and fires the processing topics. Caller has already checked that no channel
// exists yet. trigger is "status" or "notifbuddy".
//
// Errors before the channel is created are returned so the event can be
// redelivered and retried; once the channel exists, later failures are only
// logged — a retry would create a duplicate channel, which is worse than a
// missing mapping or invite.
func (e *Engine) ensureChannel(ctx context.Context, orgID, issueID string, settings integrations.LinearSettings, evt template.Event, trigger string, extras channelInviteExtras) error {
	// Condition gate (if configured): must evaluate true to proceed. Eval errors
	// are deterministic (bad expression), so retrying can't help.
	// @notifbuddy is explicit user intent — never re-apply the auto-create
	// condition on a manual command.
	if trigger != "notifbuddy" && settings.ConditionExpr != "" {
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

	// Backlink the channel to its Linear issue via the topic, and seed it with
	// the ticket body (NOT-11). Best-effort: the channel already exists, so a
	// failure must not redeliver.
	if topic := e.channelTopic(ctx, settings, evt); topic != "" {
		e.setChannelTopic(ctx, orgID, issueID, token, channelID, topic)
	}
	e.postChannelIntro(ctx, orgID, issueID, token, channelID)

	// Invite configured AutoAddMembers plus Linear issue people (creator,
	// assignee, profile @mentions) and any call-site extras. Best-effort:
	// invite failure must not redeliver (channel already exists).
	inviteIDs := e.collectChannelInvitees(ctx, orgID, issueID, token, settings.AutoAddMembers, extras)
	if len(inviteIDs) > 0 {
		if err := e.slack.InviteUsers(ctx, token, channelID, inviteIDs); err != nil {
			slog.ErrorContext(ctx, "sync: ensureChannel: invite members failed", "org_id", orgID, "channel_id", channelID, "error", err)
		} else {
			e.fireBots(ctx, orgID, channelID, inviteIDs)
		}
	}
	return nil
}

// collectChannelInvitees builds the deduped Slack user id list to invite into
// a new channel: AutoAddMembers ∪ email→Slack(Linear invitees + extras.Emails)
// ∪ extras.SlackIDs. Lookup failures are skipped.
func (e *Engine) collectChannelInvitees(ctx context.Context, orgID, issueID, token string, autoAdd []string, extras channelInviteExtras) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range autoAdd {
		add(id)
	}
	for _, id := range extras.SlackIDs {
		add(id)
	}

	emails := map[string]struct{}{}
	addEmail := func(email string) {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			return
		}
		emails[email] = struct{}{}
	}
	for _, email := range extras.Emails {
		addEmail(email)
	}
	if invitees, err := e.intg.LinearIssueInvitees(ctx, orgID, issueID, extras.Bodies...); err != nil {
		slog.WarnContext(ctx, "sync: ensureChannel: fetch issue invitees failed",
			"org_id", orgID, "issue_id", issueID, "error", err)
	} else {
		for _, inv := range invitees {
			addEmail(inv.Email)
		}
	}
	for email := range emails {
		u, err := e.slack.LookupUserByEmail(ctx, token, email)
		if err != nil || u.ID == "" {
			continue
		}
		add(u.ID)
	}
	return out
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

// slackTopicMaxLen is Slack's channel-topic length cap.
const slackTopicMaxLen = 250

// channelTopic renders the config's topic template (or the built-in default)
// against the event, trimmed to Slack's topic cap. A render failure returns ""
// (logged): a broken custom template must not block channel handling.
func (e *Engine) channelTopic(ctx context.Context, settings integrations.LinearSettings, evt template.Event) string {
	topic, err := e.tmpl.Render(settings.EffectiveTopicTemplate(), evt)
	if err != nil {
		slog.WarnContext(ctx, "sync: channel topic render failed", "error", err)
		return ""
	}
	topic = strings.TrimSpace(topic)
	if r := []rune(topic); len(r) > slackTopicMaxLen {
		topic = string(r[:slackTopicMaxLen-1]) + "…"
	}
	return topic
}

// setChannelTopic sets the channel topic and records it, so the live-update
// path can skip Slack when nothing changed. Both steps are best-effort.
func (e *Engine) setChannelTopic(ctx context.Context, orgID, issueID, token, channelID, topic string) {
	if err := e.slack.SetChannelTopic(ctx, token, channelID, topic); err != nil {
		slog.ErrorContext(ctx, "sync: set channel topic failed", "org_id", orgID, "channel_id", channelID, "error", err)
		return
	}
	if err := e.store.SetIssueChannelTopic(ctx, orgID, issueID, topic); err != nil {
		slog.ErrorContext(ctx, "sync: record channel topic failed", "org_id", orgID, "issue_id", issueID, "error", err)
	}
}

// introDescriptionMaxLen caps the ticket body quoted in the intro message
// (Slack rejects text sections beyond ~3000 chars).
const introDescriptionMaxLen = 2800

// postChannelIntro posts the new channel's first message: a link back to the
// Linear issue plus the ticket body, so the channel starts with the issue's
// context. Best-effort — the channel exists, so failures only log.
func (e *Engine) postChannelIntro(ctx context.Context, orgID, issueID, token, channelID string) {
	issue, err := e.intg.LinearIssueByID(ctx, orgID, issueID)
	if err != nil {
		slog.WarnContext(ctx, "sync: channel intro: fetch issue failed", "org_id", orgID, "issue_id", issueID, "error", err)
		return
	}
	title := strings.TrimSpace(issue.Title)
	if title == "" {
		title = issue.Identifier
	}
	if issue.Identifier == "" && title == "" {
		return
	}
	head := fmt.Sprintf("*%s: %s*", issue.Identifier, title)
	if issue.URL != "" {
		head = fmt.Sprintf("*<%s|%s: %s>*", issue.URL, issue.Identifier, title)
	}
	body := strings.TrimSpace(issue.Description)
	if r := []rune(body); len(r) > introDescriptionMaxLen {
		body = string(r[:introDescriptionMaxLen-1]) + "…"
	}
	text := head
	if body != "" {
		text += "\n\n" + body
	}
	if _, err := e.slack.PostMessage(ctx, token, slackapi.PostOptions{ChannelID: channelID, Text: text}); err != nil {
		slog.ErrorContext(ctx, "sync: channel intro: post failed", "org_id", orgID, "channel_id", channelID, "error", err)
	}
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
		// Fallback: tkt-<identifier> from the normalized issue object.
		if id, ok := evt.Linear["issue"].(map[string]any); ok {
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
