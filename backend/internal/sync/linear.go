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
	"xolo/backend/internal/intent"
	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/slackapi"
	"xolo/backend/internal/store"
	"xolo/backend/internal/template"
)

// sourceLinear is the event_source value for Linear-originated objects, in
// the same vocabulary as the webhook envelopes.
const sourceLinear = "linear"

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

// linearIssueEntity is the subset of linear.issue we read (Issue events, or
// the injected issue on Comment events).
type linearIssueEntity struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      struct {
		Name string `json:"name"`
	} `json:"state"`
	TeamID string `json:"teamId"`
	Team   struct {
		ID string `json:"id"`
	} `json:"team"`
	// botActor is present when the action was performed by an OAuth app
	// (actor=app) — i.e. by us. Its presence is the Defense-1 signal.
	BotActor *json.RawMessage `json:"botActor"`
}

// linearCommentEntity is linear.comment (former Comment webhook data).
type linearCommentEntity struct {
	ID       string           `json:"id"`
	Body     string           `json:"body"`
	IssueID  string           `json:"issueId"`
	BotActor *json.RawMessage `json:"botActor"`
	Issue    struct {
		ID string `json:"id"`
	} `json:"issue"`
	ParentID string `json:"parentId"`
	Parent   struct {
		ID string `json:"id"`
	} `json:"parent"`
}

// linearWorkflowStateEntity is linear.workflow_state.
type linearWorkflowStateEntity struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Color    string  `json:"color"`
	Position float64 `json:"position"`
	TeamID   string  `json:"teamId"`
	Team     struct {
		ID string `json:"id"`
	} `json:"team"`
}

// linearReactionEntity is linear.reaction (comment reaction webhooks).
type linearReactionEntity struct {
	ID        string `json:"id"`
	Emoji     string `json:"emoji"`
	CommentID string `json:"commentId"`
	UserID    string `json:"userId"`
}

// linearPayload is the stored event envelope after WriteLinearWebhook
// normalizes Linear's webhook (lowercase type, typed entity keys, comment→issue
// injection) and wraps it under `linear` with a top-level `event_source`.
type linearPayload struct {
	EventSource string `json:"event_source"`
	Linear      struct {
		Action        string                     `json:"action"`
		Type          string                     `json:"type"`
		Actor         linearActor                `json:"actor"`
		Issue         *linearIssueEntity         `json:"issue,omitempty"`
		Comment       *linearCommentEntity       `json:"comment,omitempty"`
		WorkflowState *linearWorkflowStateEntity `json:"workflow_state,omitempty"`
		Reaction      *linearReactionEntity      `json:"reaction,omitempty"`
	} `json:"linear"`
}

func (p linearPayload) botActor() *json.RawMessage {
	if p.Linear.Comment != nil && p.Linear.Comment.BotActor != nil {
		return p.Linear.Comment.BotActor
	}
	if p.Linear.Issue != nil && p.Linear.Issue.BotActor != nil {
		return p.Linear.Issue.BotActor
	}
	return nil
}

// OnLinearEvent is the subscriber for integrations.linear.webhook_event. A
// returned error nacks the message so it is redelivered and retried; permanent
// skips (bad payloads, unmapped orgs, our own echoes) return nil so the event
// is consumed.
func (e *Engine) OnLinearEvent(ctx context.Context, msg pubsub.Message) error {
	var ref linearEventRef
	if err := json.Unmarshal(msg.Payload, &ref); err != nil {
		slog.WarnContext(ctx, "sync: linear event: bad ref", "error", err)
		return nil
	}
	if ref.OrgID == "" {
		return nil // can't act without knowing the org
	}
	if e.orgLocked(ctx, ref.OrgID) {
		slog.InfoContext(ctx, "sync: linear event dropped: org locked (billing)", "delivery_id", ref.DeliveryID, "org_id", ref.OrgID)
		return nil
	}
	if e.orgSyncDisabled(ctx, ref.OrgID) {
		slog.InfoContext(ctx, "sync: linear event dropped: sync disabled", "delivery_id", ref.DeliveryID, "org_id", ref.OrgID)
		return nil
	}

	// Load the full stored payload (the ingestion topic carries only routing).
	// The writer persisted it before publishing the envelope, so a failure here
	// is transient and worth a retry.
	raw, err := e.store.LinearWebhookPayload(ctx, ref.DeliveryID)
	if err != nil {
		return fmt.Errorf("linear event %s: load payload: %w", ref.DeliveryID, err)
	}
	var p linearPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		slog.WarnContext(ctx, "sync: linear event: parse payload failed", "delivery_id", ref.DeliveryID, "error", err)
		return nil
	}

	// Defense 1: drop events our own Linear app caused. When we create a comment
	// with actor=app, the resulting webhook carries a botActor — dropping it
	// stops the echo from bouncing back into Slack. Reaction webhooks use
	// actor.type instead (see reaction case below).
	if p.Linear.Type != "reaction" && p.botActor() != nil {
		return nil
	}

	switch p.Linear.Type {
	case "issue":
		return e.onLinearIssue(ctx, ref.OrgID, p)
	case "comment":
		return e.onLinearComment(ctx, ref.OrgID, p)
	case "workflow_state":
		return e.onLinearWorkflowState(ctx, ref.OrgID, p)
	case "reaction":
		// Defense 1 for reactions: only mirror human-authored reactions.
		// App/OAuth echoes from our reactionCreate have a non-user actor.
		if p.Linear.Actor.Type != "" && p.Linear.Actor.Type != "user" {
			return nil
		}
		return e.onLinearReaction(ctx, ref.OrgID, p)
	}
	return nil
}

// onLinearIssue handles the channel-creation and channel-archive rules. One
// issue event is checked in both directions: an issue that already has a
// channel is only ever a candidate for archiving (never re-creation), and an
// issue without one is only a candidate for creation. Templates and conditions
// evaluate against the forwarded event envelope, exactly as the settings test
// UI does.
func (e *Engine) onLinearIssue(ctx context.Context, orgID string, p linearPayload) error {
	if p.Linear.Issue == nil {
		return nil
	}
	settings, ok := e.settingForIssue(ctx, orgID, p)
	if !ok {
		return nil // no config applies to this issue's team
	}
	issueID := p.Linear.Issue.ID
	evt := template.Event{EventType: "linear", Linear: envelopeLinear(p)}
	stateName := p.Linear.Issue.State.Name

	// Serialize concurrent deliveries of this same issue: Pub/Sub push is
	// at-least-once and concurrent, so without this two deliveries could both
	// see "no channel" and both create a Slack channel. The lock is scoped to
	// (org, issue), so different issues still process in parallel.
	unlock, err := e.store.LockIssue(ctx, orgID, issueID)
	if err != nil {
		return fmt.Errorf("onLinearIssue: lock: %w", err) // transient; nack and retry
	}
	defer unlock()

	// Idempotency: one channel per issue. An existing channel is never
	// re-created; it can only be archived by the archive trigger. The trigger
	// rules live in integrations.{Create,Archive}Triggered, shared with the
	// settings test panel so "Run test" and the engine can never disagree.
	switch mapping, err := e.store.IssueChannelForIssue(ctx, orgID, issueID); {
	case err == nil:
		archive, err := integrations.ArchiveTriggered(e.tmpl, settings, stateName, evt)
		if err != nil {
			slog.WarnContext(ctx, "sync: archive trigger eval failed", "org_id", orgID, "issue_id", issueID, "error", err)
			return nil // deterministic eval error; retrying can't help
		}
		if archive {
			return e.closeChannel(ctx, orgID, issueID)
		}
		// Live-sync the topic backlink from issue updates (NOT-11): re-render
		// and only call Slack when the topic actually changed. Best-effort —
		// the event's real work is done.
		if topic := e.channelTopic(ctx, settings, evt); topic != "" && topic != mapping.Topic {
			token, err := e.intg.SlackBotToken(ctx, orgID)
			if err != nil {
				slog.WarnContext(ctx, "sync: topic update: slack token failed", "org_id", orgID, "error", err)
				return nil
			}
			e.setChannelTopic(ctx, orgID, issueID, token, mapping.SlackChannelID, topic)
		}
		return nil
	case errors.Is(err, store.ErrNotFound):
		// No channel yet — fall through to the creation path below.
	default:
		// A transient lookup error must NOT be treated as "no channel", or a
		// hiccup would create a duplicate for an issue that already has one.
		return fmt.Errorf("onLinearIssue: channel lookup: %w", err)
	}

	create, err := integrations.CreateTriggered(e.tmpl, settings, stateName, evt)
	if err != nil {
		slog.WarnContext(ctx, "sync: create trigger eval failed", "org_id", orgID, "issue_id", issueID, "error", err)
		return nil // deterministic eval error; retrying can't help
	}
	if !create {
		return nil
	}
	return e.ensureChannel(ctx, orgID, issueID, settings, evt, settings.CreationMode, channelInviteExtras{})
}

// settingForIssue resolves the config that applies to an issue event's team.
// Returns ok=false (and logs only real errors) when the team is unmapped —
// an unmapped team is an explicit "do nothing", not an error.
func (e *Engine) settingForIssue(ctx context.Context, orgID string, p linearPayload) (integrations.LinearSettings, bool) {
	if p.Linear.Issue == nil {
		return integrations.LinearSettings{}, false
	}
	teamID := p.Linear.Issue.TeamID
	if teamID == "" {
		teamID = p.Linear.Issue.Team.ID
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
		slog.ErrorContext(ctx, "sync: linear: resolve setting for team failed", "org_id", orgID, "team_id", teamID, "error", err)
		return integrations.LinearSettings{}, false
	}
	return settings, true
}

// onLinearWorkflowState keeps the org's synced status snapshot fresh: a
// create/update upserts the state into its team's list; a remove deletes it.
// This is what powers the settings status dropdown between full syncs.
func (e *Engine) onLinearWorkflowState(ctx context.Context, orgID string, p linearPayload) error {
	d := p.Linear.WorkflowState
	if d == nil {
		return nil
	}
	teamID := d.Team.ID
	if teamID == "" {
		teamID = d.TeamID
	}
	if teamID == "" || d.ID == "" {
		return nil
	}
	st := store.LinearWorkflowState{
		ID: d.ID, Name: d.Name, Type: d.Type, Color: d.Color, Position: d.Position,
	}
	removed := p.Linear.Action == "remove"
	// The patch is an idempotent upsert/delete, so a transient DB failure is
	// safe to retry via redelivery.
	if err := e.store.PatchLinearTeamState(ctx, orgID, teamID, st, removed); err != nil {
		return fmt.Errorf("linear workflow state %s (team %s): %w", d.ID, teamID, err)
	}
	return nil
}

// linearUploadMD matches a markdown image or link whose target is Linear's
// private upload host — the form Linear uses to embed comment attachments.
// Captures: [1] link text (filename), [2] URL.
var linearUploadMD = regexp.MustCompile(`!?\[([^\]]*)\]\((https://uploads\.linear\.app/[^)\s]+)\)`)

// linearUpload is one private-upload embed found in a comment body.
type linearUpload struct {
	markdown string // the full markdown token, for stripping from the text
	name     string
	url      string
	image    bool // ![...] image embed vs [...] file link
}

func parseLinearUploads(body string) []linearUpload {
	var out []linearUpload
	for _, m := range linearUploadMD.FindAllStringSubmatch(body, -1) {
		name := m[1]
		if name == "" {
			name = "attachment"
		}
		out = append(out, linearUpload{
			markdown: m[0], name: name, url: m[2],
			image: strings.HasPrefix(m[0], "!"),
		})
	}
	return out
}

// onLinearComment mirrors a human Linear comment into the issue's Slack channel,
// or handles an @notifbuddy command in the comment body. Errors before the
// Slack post are returned for retry; failures after it are only logged so a
// redelivery can't double-post.
func (e *Engine) onLinearComment(ctx context.Context, orgID string, p linearPayload) error {
	d := p.Linear.Comment
	if d == nil {
		return nil
	}
	if p.Linear.Action == "update" {
		// Text edits are out of scope, but Linear attaches comment files
		// asynchronously — the embed lands in an update seconds after create —
		// so updates are scanned for not-yet-synced uploads.
		return e.onLinearCommentUpdate(ctx, orgID, p)
	}
	if p.Linear.Action != "create" {
		return nil // removes etc. are out of scope
	}
	issueID := d.IssueID
	if issueID == "" {
		issueID = d.Issue.ID
	}
	if issueID == "" && p.Linear.Issue != nil {
		issueID = p.Linear.Issue.ID
	}
	if issueID == "" {
		return nil
	}

	// @notifbuddy command? Classify the body; a create/close command short-
	// circuits mirroring. The normalized envelope already carries linear.issue
	// (injected at ingest) for naming templates.
	if e.handleNotifBuddy(ctx, orgID, issueID, d.Body, &p) {
		return nil
	}

	// Otherwise mirror the comment into the channel (if the issue has one).
	channelID, err := e.store.ChannelForIssue(ctx, orgID, issueID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // no channel for this issue; nothing to mirror to
	}
	if err != nil {
		return fmt.Errorf("linear comment: channel lookup: %w", err)
	}

	// Idempotency: if this comment was already mirrored (Pub/Sub redelivers a
	// slow-but-successful message after the ack deadline), don't post it again.
	// The link is keyed on the comment's own id, so this is exact.
	if _, err := e.store.LinkByLinearComment(ctx, orgID, d.ID); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("linear comment: mirror lookup: %w", err)
	}

	botToken, err := e.intg.SlackBotToken(ctx, orgID)
	if err != nil {
		return fmt.Errorf("linear comment: slack token: %w", err)
	}

	postToken, err := e.slackCommentPostAuth(ctx, orgID, p.Linear.Actor)
	if errors.Is(err, store.ErrNotFound) {
		slog.InfoContext(ctx, "sync: linear comment: skip unlinked user",
			"org_id", orgID, "comment_id", d.ID, "linear_user", p.Linear.Actor.ID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("linear comment: slack token: %w", err)
	}

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

	text, images, fileShares := e.pullLinearUploads(ctx, orgID, d.ID, d.Body, nil)
	text = strings.TrimSpace(text)
	if text == "" && (len(images) > 0 || len(fileShares) > 0) {
		text = "📎 shared from Linear"
	}

	ts, err := e.slack.PostMessage(ctx, postToken, slackapi.PostOptions{
		ChannelID: channelID,
		Text:      text,
		ThreadTS:  threadTS,
		Blocks:    commentBlocks(text, images),
	})
	if err != nil {
		return fmt.Errorf("linear comment: post to slack: %w", err)
	}

	// The message exists now; asset bookkeeping and thread shares are
	// best-effort (a redelivery would double-post the text). Each synced asset
	// is recorded so the follow-up comment update (Linear re-sends the body)
	// doesn't sync the same file again.
	for _, img := range images {
		e.recordAsset(ctx, orgID, d.ID, img.asset)
	}
	fileThread := firstNonEmpty(threadTS, ts)
	e.shareFiles(ctx, orgID, d.ID, botToken, channelID, fileThread, fileShares)

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
		slog.ErrorContext(ctx, "sync: linear comment: record link failed", "org_id", orgID, "comment_id", d.ID, "channel_id", channelID, "error", err)
	}

	e.fireMessage(ctx, orgID, TopicSlackMessage, MessageEvent{
		OrgID:           orgID,
		Direction:       "linear->slack",
		LinearIssueID:   issueID,
		LinearCommentID: d.ID,
		SlackChannel:    channelID,
		SlackTS:         ts,
	})
	return nil
}

// onLinearCommentUpdate syncs attachments that Linear appended to an
// already-mirrored comment. Linear uploads comment files asynchronously: the
// create webhook carries only the text, and the ![...] embed arrives in a
// Comment update seconds later. New images are grafted onto the mirrored
// message itself (chat.update with rebuilt blocks) so text + image stay one
// entity; other files share into the thread. Only uploads not yet in
// mirrored_assets sync — text edits never re-post, redelivered updates no-op.
func (e *Engine) onLinearCommentUpdate(ctx context.Context, orgID string, p linearPayload) error {
	d := p.Linear.Comment
	if d == nil {
		return nil
	}
	uploads := parseLinearUploads(d.Body)
	if len(uploads) == 0 {
		return nil
	}
	link, err := e.store.LinkByLinearComment(ctx, orgID, d.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // comment was never mirrored (unmapped issue, or predates sync)
	}
	if err != nil {
		return fmt.Errorf("linear comment update: mirror lookup: %w", err)
	}
	synced, err := e.store.MirroredAssets(ctx, orgID, sourceLinear, d.ID)
	if err != nil {
		return fmt.Errorf("linear comment update: synced assets: %w", err)
	}
	syncedURL := map[string]bool{}
	for _, a := range synced {
		syncedURL[a.AssetURL] = true
	}
	anyFresh := false
	for _, u := range uploads {
		if !syncedURL[u.url] {
			anyFresh = true
			break
		}
	}
	if !anyFresh {
		return nil
	}

	botToken, err := e.intg.SlackBotToken(ctx, orgID)
	if err != nil {
		return fmt.Errorf("linear comment update: slack token: %w", err)
	}
	updateToken, err := e.slackCommentPostAuth(ctx, orgID, p.Linear.Actor)
	if errors.Is(err, store.ErrNotFound) {
		slog.InfoContext(ctx, "sync: linear comment: skip unlinked user",
			"org_id", orgID, "comment_id", d.ID, "linear_user", p.Linear.Actor.ID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("linear comment update: slack token: %w", err)
	}

	text, newImages, newFiles := e.pullLinearUploads(ctx, orgID, d.ID, d.Body, syncedURL)
	if len(newImages) > 0 {
		// Rebuild the mirrored message as one entity: the comment's current
		// text plus every synced image (previously grafted ones included).
		text = strings.TrimSpace(text)
		if text == "" {
			text = "📎 shared from Linear"
		}
		allImages := append(e.inlineImagesFor(ctx, orgID, synced), newImages...)
		if err := e.slack.UpdateMessage(ctx, updateToken, slackapi.UpdateOptions{
			ChannelID: link.SlackChannelID,
			TS:        link.SlackTS,
			Text:      text,
			Blocks:    commentBlocks(text, allImages),
		}); err != nil {
			// Not recorded → the image is lost for this delivery; loud log
			// rather than a separate app post the user explicitly didn't want.
			slog.ErrorContext(ctx, "sync: linear comment update: message update failed",
				"org_id", orgID, "comment_id", d.ID, "error", err)
		} else {
			for _, img := range newImages {
				e.recordAsset(ctx, orgID, d.ID, img.asset)
			}
		}
	}
	e.shareFiles(ctx, orgID, d.ID, botToken, link.SlackChannelID, firstNonEmpty(link.RootSlackTS, link.SlackTS), newFiles)
	return nil
}

func (e *Engine) slackCommentPostAuth(ctx context.Context, orgID string, actor linearActor) (string, error) {
	token, _, _, err := e.slackReactionToken(ctx, orgID, actor.ID)
	return token, err
}

// inlineImage is an image embed rendered inside the mirrored message's blocks:
// Slack fetches proxyURL (our signed backend proxy) to display it inline.
type inlineImage struct {
	asset    store.MirroredAsset
	proxyURL string
}

// pulledFile is a non-image download pending a thread share.
type pulledFile struct {
	assetURL string
	name     string
	data     []byte
}

// pullLinearUploads resolves a body's private Linear uploads (except those in
// skip, whose markdown is stripped without reprocessing). Images become proxy
// URLs for block rendering — no bytes move. Non-image files are downloaded for
// a thread share. It returns the body text with all handled embeds stripped,
// plus both lists. Failures log and leave the markdown intact.
func (e *Engine) pullLinearUploads(ctx context.Context, orgID, commentID, body string, skip map[string]bool) (string, []inlineImage, []pulledFile) {
	text := body
	var images []inlineImage
	var files []pulledFile
	for _, u := range parseLinearUploads(body) {
		if skip[u.url] {
			text = strings.Replace(text, u.markdown, "", 1)
			continue
		}
		if u.image {
			proxyURL, err := e.intg.LinearAssetProxyURL(orgID, u.url)
			if err != nil {
				slog.ErrorContext(ctx, "sync: linear comment: asset proxy url failed",
					"org_id", orgID, "comment_id", commentID, "error", err)
				continue
			}
			images = append(images, inlineImage{
				asset:    store.MirroredAsset{AssetURL: u.url, Filename: u.name, Inline: true},
				proxyURL: proxyURL,
			})
		} else {
			data, _, err := e.intg.LinearFileDownload(ctx, orgID, u.url)
			if err != nil {
				slog.ErrorContext(ctx, "sync: linear comment: attachment download failed",
					"org_id", orgID, "comment_id", commentID, "error", err)
				continue
			}
			files = append(files, pulledFile{assetURL: u.url, name: u.name, data: data})
		}
		text = strings.Replace(text, u.markdown, "", 1)
	}
	return text, images, files
}

// inlineImagesFor rebuilds the block-image list for previously synced inline
// assets — the proxy URL is re-derived from the asset URL.
func (e *Engine) inlineImagesFor(ctx context.Context, orgID string, assets []store.MirroredAsset) []inlineImage {
	var out []inlineImage
	for _, a := range assets {
		if !a.Inline {
			continue
		}
		proxyURL, err := e.intg.LinearAssetProxyURL(orgID, a.AssetURL)
		if err != nil {
			slog.ErrorContext(ctx, "sync: linear comment: asset proxy url failed",
				"org_id", orgID, "error", err)
			continue
		}
		out = append(out, inlineImage{asset: a, proxyURL: proxyURL})
	}
	return out
}

// commentBlocks composes the one-entity layout: a text section followed by an
// image block per synced image. nil when there are no images (plain text
// message, no blocks needed).
func commentBlocks(text string, images []inlineImage) []map[string]any {
	if len(images) == 0 {
		return nil
	}
	var blocks []map[string]any
	if text != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": text},
		})
	}
	for _, img := range images {
		alt := img.asset.Filename
		if alt == "" {
			alt = "attachment"
		}
		blocks = append(blocks, map[string]any{
			"type":      "image",
			"image_url": img.proxyURL,
			"alt_text":  alt,
		})
	}
	return blocks
}

func (e *Engine) recordAsset(ctx context.Context, orgID, commentID string, a store.MirroredAsset) {
	if err := e.store.RecordMirroredAsset(ctx, orgID, sourceLinear, commentID, a); err != nil {
		slog.ErrorContext(ctx, "sync: linear comment: record asset failed",
			"org_id", orgID, "comment_id", commentID, "error", err)
	}
}

// shareFiles shares non-image files into the given thread, recording each
// success so later updates don't re-share them. Best-effort per file.
func (e *Engine) shareFiles(ctx context.Context, orgID, commentID, token, channelID, threadTS string, files []pulledFile) {
	for _, f := range files {
		if err := e.slack.UploadFile(ctx, token, slackapi.UploadOptions{
			ChannelID: channelID, ThreadTS: threadTS, Filename: f.name, Data: f.data,
		}); err != nil {
			slog.ErrorContext(ctx, "sync: linear comment: slack file upload failed",
				"org_id", orgID, "comment_id", commentID, "filename", f.name, "error", err)
			continue
		}
		e.recordAsset(ctx, orgID, commentID, store.MirroredAsset{AssetURL: f.assetURL, Filename: f.name})
	}
}

// handleNotifBuddy resolves our Slack bot identity for the org, detects a bot
// mention in body, and runs create/close commands. Returns true if the body
// was a command (mirroring should stop). Commands stay best-effort: failures
// are logged, never retried via redelivery — re-running the classifier on a
// redelivered comment could re-execute a command the user already saw take effect.
//
// p is the normalized comment envelope (already includes injected issue when
// ingest succeeded). On create fallback it may set p.Linear.Issue. Callers on
// the Slack path may pass nil; create then fetches the issue via GraphQL.
func (e *Engine) handleNotifBuddy(ctx context.Context, orgID, issueID, body string, p *linearPayload) bool {
	if e.classifier == nil {
		return false
	}
	token, err := e.intg.SlackBotToken(ctx, orgID)
	if err != nil {
		slog.WarnContext(ctx, "sync: notifbuddy: slack token failed; skipping NLP",
			"org_id", orgID, "error", err)
		return false
	}
	bot, ok := e.resolveBotIdentity(ctx, orgID, token)
	if !ok || !botMentioned(body, bot.SlackUserID, bot.SlackDisplayName) {
		return false
	}
	return e.runNotifBuddyCommand(ctx, orgID, issueID, body, "", p)
}

// runNotifBuddyCommand classifies body and performs create/close. Caller has
// already verified the bot was mentioned. Returns true when the body was a
// create/close command (mirroring should stop). slackAuthorID is the Slack user
// who typed the command when the create originated in Slack (empty on Linear).
func (e *Engine) runNotifBuddyCommand(ctx context.Context, orgID, issueID, body, slackAuthorID string, p *linearPayload) bool {
	if p == nil {
		p = &linearPayload{}
		p.Linear.Type = "comment"
	}
	switch e.classifier.Classify(ctx, body) {
	case intent.CreateChannel:
		teamID := teamIDFromIssue(p.Linear.Issue)
		if teamID == "" {
			// Fallback when ingest couldn't inject the issue (e.g. org unknown)
			// or the Slack path has no Linear envelope.
			issue, err := e.intg.LinearIssueByID(ctx, orgID, issueID)
			if err != nil {
				slog.ErrorContext(ctx, "sync: notifbuddy create: fetch issue failed", "org_id", orgID, "issue_id", issueID, "error", err)
				return true
			}
			teamID = issue.TeamID
			p.Linear.Type = "comment"
			p.Linear.Issue = issueEntityFromLinearIssue(issue)
		}
		settings, ok := e.settingForTeam(ctx, orgID, teamID)
		if !ok {
			return true // no config applies to this issue's team
		}
		evt := template.Event{EventType: "linear", Linear: envelopeLinear(*p)}
		extras := channelInviteExtras{
			Bodies:   []string{body},
			SlackIDs: []string{slackAuthorID},
		}
		if email := strings.TrimSpace(p.Linear.Actor.Email); email != "" {
			extras.Emails = []string{email}
		}
		if _, err := e.store.ChannelForIssue(ctx, orgID, issueID); err != nil {
			if err := e.ensureChannel(ctx, orgID, issueID, settings, evt, "notifbuddy", extras); err != nil {
				slog.ErrorContext(ctx, "sync: notifbuddy create failed", "org_id", orgID, "issue_id", issueID, "error", err)
			}
		}
		return true
	case intent.CloseChannel:
		if err := e.closeChannel(ctx, orgID, issueID); err != nil {
			slog.ErrorContext(ctx, "sync: notifbuddy close failed", "org_id", orgID, "issue_id", issueID, "error", err)
		}
		return true
	default:
		return false
	}
}

// teamIDFromIssue reads the team id from a typed linear.issue entity.
func teamIDFromIssue(issue *linearIssueEntity) string {
	if issue == nil {
		return ""
	}
	if issue.TeamID != "" {
		return issue.TeamID
	}
	return issue.Team.ID
}

// issueEntityFromLinearIssue maps the integrations fetch result onto the
// sync engine's typed issue entity (for template naming / team resolve).
func issueEntityFromLinearIssue(issue integrations.LinearIssue) *linearIssueEntity {
	ent := &linearIssueEntity{
		ID:         issue.ID,
		Identifier: issue.Identifier,
		Title:      issue.Title,
		TeamID:     issue.TeamID,
	}
	ent.State.Name = issue.StateName
	ent.Team.ID = issue.TeamID
	return ent
}

// envelopeLinear rebuilds the normalized linear object the template engine
// walks, from the typed payload. We round-trip through JSON so the template
// sees the same shape the settings test UI does.
func envelopeLinear(p linearPayload) map[string]any {
	b, _ := json.Marshal(p.Linear)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
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
