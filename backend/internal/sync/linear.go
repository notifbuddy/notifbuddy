package sync

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"

	"xolo/backend/internal/integrations"
	"xolo/backend/internal/intent"
	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/slackapi"
	"xolo/backend/internal/store"
	"xolo/backend/internal/template"
)

// linearEventRef is the routing envelope published on the ingestion topic. The
// engine re-reads the full stored payload for the event body.
type linearEventRef struct {
	DeliveryID string `json:"delivery_id"`
	EventType  string `json:"event_type"`
	Action     string `json:"action"`
	OrgID      string `json:"org_id"`
}

// linearActor identifies who caused an event.
type linearActor struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Type  string `json:"type"`
}

// linearData is the subset of a Linear webhook's `data` we read, covering Issue
// events (identifier/state/team), Comment events (body/issue/parent), and
// WorkflowState events (id/name/type/color/position/team).
type linearData struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	State      struct {
		Name string `json:"name"`
	} `json:"state"`
	// TeamID identifies the issue's (or workflow state's) team. Present on Issue
	// events (data.teamId) and used to resolve which config applies.
	TeamID string `json:"teamId"`
	Team   struct {
		ID string `json:"id"`
	} `json:"team"`
	// WorkflowState fields (type == "WorkflowState"): the status itself.
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Color    string  `json:"color"`
	Position float64 `json:"position"`
	// botActor is present when the action was performed by an OAuth app
	// (actor=app) — i.e. by us. Its presence is the Defense-1 signal.
	BotActor *json.RawMessage `json:"botActor"`
	// Comment fields.
	Body    string `json:"body"`
	IssueID string `json:"issueId"`
	Issue   struct {
		ID string `json:"id"`
	} `json:"issue"`
	ParentID string `json:"parentId"`
	Parent   struct {
		ID string `json:"id"`
	} `json:"parent"`
}

// linearPayload is the stored webhook body ({event_type, linear:{...}}).
type linearPayload struct {
	Linear struct {
		Action string      `json:"action"`
		Type   string      `json:"type"`
		Actor  linearActor `json:"actor"`
		Data   linearData  `json:"data"`
	} `json:"linear"`
}

// OnLinearEvent is the subscriber for integrations.linear.webhook_event. It is
// invoked on the bus's delivery goroutine; it does the work synchronously and
// logs failures (there is no caller to return an error to).
func (e *Engine) OnLinearEvent(ctx context.Context, msg pubsub.Message) {
	var ref linearEventRef
	if err := json.Unmarshal(msg.Payload, &ref); err != nil {
		log.Printf("sync: linear event: bad ref: %v", err)
		return
	}
	if ref.OrgID == "" {
		return // can't act without knowing the org
	}

	// Load the full stored payload (the ingestion topic carries only routing).
	raw, err := e.store.LinearWebhookPayload(ctx, ref.DeliveryID)
	if err != nil {
		log.Printf("sync: linear event %s: load payload: %v", ref.DeliveryID, err)
		return
	}
	var p linearPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		log.Printf("sync: linear event %s: parse payload: %v", ref.DeliveryID, err)
		return
	}

	// Defense 1: drop events our own Linear app caused. When we create a comment
	// with actor=app, the resulting webhook carries a botActor — dropping it
	// stops the echo from bouncing back into Slack.
	if p.Linear.Data.BotActor != nil {
		return
	}

	switch p.Linear.Type {
	case "Issue":
		e.onLinearIssue(ctx, ref.OrgID, p)
	case "Comment":
		e.onLinearComment(ctx, ref.OrgID, raw, p)
	case "WorkflowState":
		e.onLinearWorkflowState(ctx, ref.OrgID, p)
	}
}

// onLinearIssue handles the status-trigger channel-creation rule. It renders the
// name template and evaluates the condition from the org's saved settings, both
// against the forwarded event envelope, exactly as the settings test UI does.
func (e *Engine) onLinearIssue(ctx context.Context, orgID string, p linearPayload) {
	settings, ok := e.settingForIssue(ctx, orgID, p)
	if !ok {
		return // no config applies to this issue's team
	}
	switch settings.CreationMode {
	case "status":
		// Create when the issue reaches the configured workflow state.
		if !strings.EqualFold(p.Linear.Data.State.Name, settings.TriggerStatus) {
			return // not the trigger status
		}
	case "condition":
		// Create whenever the condition expression is true; the condition gate in
		// ensureChannel does the actual evaluation, so nothing to check here.
	default:
		return // manual mode: channels are created via @notifbuddy only
	}
	issueID := p.Linear.Data.ID
	// Idempotency: one channel per issue. If it already exists, do nothing.
	if _, err := e.store.ChannelForIssue(ctx, orgID, issueID); err == nil {
		return
	}
	evt := template.Event{EventType: "linear", Linear: envelopeLinear(p)}
	e.ensureChannel(ctx, orgID, issueID, settings, evt, settings.CreationMode)
}

// settingForIssue resolves the config that applies to an issue event's team.
// Returns ok=false (and logs only real errors) when the team is unmapped —
// an unmapped team is an explicit "do nothing", not an error.
func (e *Engine) settingForIssue(ctx context.Context, orgID string, p linearPayload) (integrations.LinearSettings, bool) {
	teamID := p.Linear.Data.TeamID
	if teamID == "" {
		teamID = p.Linear.Data.Team.ID
	}
	if teamID == "" {
		return integrations.LinearSettings{}, false
	}
	return e.settingForTeam(ctx, orgID, teamID)
}

// settingForTeam wraps the integrations resolver, mapping "unmapped team"
// (store.ErrNotFound) to ok=false and logging only unexpected errors.
func (e *Engine) settingForTeam(ctx context.Context, orgID, teamID string) (integrations.LinearSettings, bool) {
	settings, err := e.intg.SettingForTeam(ctx, orgID, teamID)
	if errors.Is(err, store.ErrNotFound) {
		return integrations.LinearSettings{}, false
	}
	if err != nil {
		log.Printf("sync: linear: resolve setting for team %s: %v", teamID, err)
		return integrations.LinearSettings{}, false
	}
	return settings, true
}

// onLinearWorkflowState keeps the org's synced status snapshot fresh: a
// create/update upserts the state into its team's list; a remove deletes it.
// This is what powers the settings status dropdown between full syncs.
func (e *Engine) onLinearWorkflowState(ctx context.Context, orgID string, p linearPayload) {
	d := p.Linear.Data
	teamID := d.Team.ID
	if teamID == "" {
		teamID = d.TeamID
	}
	if teamID == "" || d.ID == "" {
		return
	}
	st := store.LinearWorkflowState{
		ID: d.ID, Name: d.Name, Type: d.Type, Color: d.Color, Position: d.Position,
	}
	removed := p.Linear.Action == "remove"
	if err := e.store.PatchLinearTeamState(ctx, orgID, teamID, st, removed); err != nil {
		log.Printf("sync: linear workflow state %s (team %s): %v", d.ID, teamID, err)
	}
}

// onLinearComment mirrors a human Linear comment into the issue's Slack channel,
// or handles an @notifbuddy command in the comment body.
func (e *Engine) onLinearComment(ctx context.Context, orgID string, raw []byte, p linearPayload) {
	d := p.Linear.Data
	if p.Linear.Action != "create" {
		return // only new comments mirror (edits/removes are out of scope)
	}
	issueID := d.IssueID
	if issueID == "" {
		issueID = d.Issue.ID
	}
	if issueID == "" {
		return
	}

	// @notifbuddy command? Classify the body; a create/close command short-
	// circuits mirroring.
	if e.handleNotifBuddy(ctx, orgID, issueID, d.Body, raw) {
		return
	}

	// Otherwise mirror the comment into the channel (if the issue has one).
	channelID, err := e.store.ChannelForIssue(ctx, orgID, issueID)
	if errors.Is(err, store.ErrNotFound) {
		return // no channel for this issue; nothing to mirror to
	}
	if err != nil {
		log.Printf("sync: linear comment: channel lookup: %v", err)
		return
	}

	token, err := e.intg.SlackBotToken(ctx, orgID)
	if err != nil {
		log.Printf("sync: linear comment: slack token: %v", err)
		return
	}

	// Resolve a thread parent: if this Linear comment is a reply, post it under
	// the Slack ts that mirrors its parent comment.
	threadTS := ""
	rootSlackTS := ""
	parentID := d.ParentID
	if parentID == "" {
		parentID = d.Parent.ID
	}
	if parentID != "" {
		if link, err := e.store.LinkByLinearComment(ctx, orgID, parentID); err == nil {
			threadTS = link.SlackTS
			rootSlackTS = firstNonEmpty(link.RootSlackTS, link.SlackTS)
		}
	}

	// Attribution: show the Linear author's name + avatar (resolved via their
	// email in Slack) while the message is authored by our bot (Defense 1).
	username := p.Linear.Actor.Name
	iconURL := ""
	if p.Linear.Actor.Email != "" {
		if u, err := e.slack.LookupUserByEmail(ctx, token, p.Linear.Actor.Email); err == nil {
			if u.Name != "" {
				username = u.Name
			}
			iconURL = u.IconURL
		}
	}

	ts, err := e.slack.PostMessage(ctx, token, slackapi.PostOptions{
		ChannelID: channelID,
		Text:      d.Body,
		Username:  username,
		IconURL:   iconURL,
		ThreadTS:  threadTS,
	})
	if err != nil {
		log.Printf("sync: linear comment: post to slack: %v", err)
		return
	}

	if rootSlackTS == "" {
		rootSlackTS = ts // this is a thread root
	}
	if err := e.store.RecordMirroredMessage(ctx, store.MirroredMessage{
		OrgID:           orgID,
		LinearCommentID: d.ID,
		SlackChannelID:  channelID,
		SlackTS:         ts,
		RootSlackTS:     rootSlackTS,
	}); err != nil {
		log.Printf("sync: linear comment: record link: %v", err)
	}

	e.fireMessage(ctx, orgID, TopicSlackMessage, MessageEvent{
		OrgID:           orgID,
		Direction:       "linear->slack",
		LinearIssueID:   issueID,
		LinearCommentID: d.ID,
		SlackChannel:    channelID,
		SlackTS:         ts,
	})
}

// handleNotifBuddy classifies a comment body and, on a create/close command,
// performs it. Returns true if the body was a command (mirroring should stop).
func (e *Engine) handleNotifBuddy(ctx context.Context, orgID, issueID, body string, raw []byte) bool {
	if e.classifier == nil || !strings.Contains(strings.ToLower(body), "notifbuddy") {
		return false
	}
	switch e.classifier.Classify(ctx, body) {
	case intent.CreateChannel:
		// Resolve which config applies via the issue's team. The comment webhook
		// doesn't reliably carry the team, so fetch the issue for it.
		issue, err := e.intg.LinearIssueByID(ctx, orgID, issueID)
		if err != nil {
			log.Printf("sync: notifbuddy create: fetch issue: %v", err)
			return true
		}
		settings, ok := e.settingForTeam(ctx, orgID, issue.TeamID)
		if !ok {
			return true // no config applies to this issue's team
		}
		evt := template.Event{EventType: "linear", Linear: envelopeLinearRaw(raw)}
		if _, err := e.store.ChannelForIssue(ctx, orgID, issueID); err != nil {
			e.ensureChannel(ctx, orgID, issueID, settings, evt, "notifbuddy")
		}
		return true
	case intent.CloseChannel:
		e.closeChannel(ctx, orgID, issueID)
		return true
	default:
		return false
	}
}

// envelopeLinear rebuilds the { action, type, actor, data } map the template
// engine walks, from the typed payload. We round-trip through JSON so the
// template sees the same shape the settings test UI does.
func envelopeLinear(p linearPayload) map[string]any {
	b, _ := json.Marshal(p.Linear)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// envelopeLinearRaw extracts the raw `linear` object from a stored payload for
// template evaluation (preserves every field, not just the typed subset).
func envelopeLinearRaw(raw []byte) map[string]any {
	var wrap struct {
		Linear map[string]any `json:"linear"`
	}
	_ = json.Unmarshal(raw, &wrap)
	return wrap.Linear
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// compile-time: the concrete service/store must satisfy the engine's interfaces.
var (
	_ Integrations = (*integrations.Service)(nil)
	_ Store        = (*store.Store)(nil)
)
