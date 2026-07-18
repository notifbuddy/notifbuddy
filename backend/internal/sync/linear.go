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

// linearPayload is the stored event envelope: the writer
// (integrations.WriteLinearWebhook) wraps Linear's raw webhook body under
// `linear` with a top-level `event_source`, so future sources and
// notifbuddy-side metadata can ride at the top level without touching the
// provider payload.
type linearPayload struct {
	EventSource string `json:"event_source"`
	Linear      struct {
		Action string      `json:"action"`
		Type   string      `json:"type"`
		Actor  linearActor `json:"actor"`
		Data   linearData  `json:"data"`
	} `json:"linear"`
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
	// stops the echo from bouncing back into Slack.
	if p.Linear.Data.BotActor != nil {
		return nil
	}

	switch p.Linear.Type {
	case "Issue":
		return e.onLinearIssue(ctx, ref.OrgID, p)
	case "Comment":
		return e.onLinearComment(ctx, ref.OrgID, raw, p)
	case "WorkflowState":
		return e.onLinearWorkflowState(ctx, ref.OrgID, p)
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
	settings, ok := e.settingForIssue(ctx, orgID, p)
	if !ok {
		return nil // no config applies to this issue's team
	}
	issueID := p.Linear.Data.ID
	evt := template.Event{EventType: "linear", Linear: envelopeLinear(p)}
	stateName := p.Linear.Data.State.Name

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
	switch _, err := e.store.ChannelForIssue(ctx, orgID, issueID); {
	case err == nil:
		archive, err := integrations.ArchiveTriggered(e.tmpl, settings, stateName, evt)
		if err != nil {
			slog.WarnContext(ctx, "sync: archive trigger eval failed", "org_id", orgID, "issue_id", issueID, "error", err)
			return nil // deterministic eval error; retrying can't help
		}
		if archive {
			return e.closeChannel(ctx, orgID, issueID)
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
	return e.ensureChannel(ctx, orgID, issueID, settings, evt, settings.CreationMode)
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
		slog.ErrorContext(ctx, "sync: linear: resolve setting for team failed", "org_id", orgID, "team_id", teamID, "error", err)
		return integrations.LinearSettings{}, false
	}
	return settings, true
}

// onLinearWorkflowState keeps the org's synced status snapshot fresh: a
// create/update upserts the state into its team's list; a remove deletes it.
// This is what powers the settings status dropdown between full syncs.
func (e *Engine) onLinearWorkflowState(ctx context.Context, orgID string, p linearPayload) error {
	d := p.Linear.Data
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
func (e *Engine) onLinearComment(ctx context.Context, orgID string, raw []byte, p linearPayload) error {
	d := p.Linear.Data
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
	if issueID == "" {
		return nil
	}

	// @notifbuddy command? Classify the body; a create/close command short-
	// circuits mirroring.
	if e.handleNotifBuddy(ctx, orgID, issueID, d.Body, raw) {
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

	token, err := e.intg.SlackBotToken(ctx, orgID)
	if err != nil {
		return fmt.Errorf("linear comment: slack token: %w", err)
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

	// Attachments: Linear embeds uploads as markdown links on uploads.linear.app,
	// which only serve with Linear auth — Slack can't render them. Re-host each
	// on Slack: images are uploaded privately and rendered inside the message's
	// own blocks (one entity — not a separate post), other files are shared into
	// the thread after the message posts. A failed download leaves the markdown
	// link in place (degraded but visible) rather than failing the mirror.
	text, images, fileShares := e.pullLinearUploads(ctx, orgID, d.ID, d.Body, nil)
	text = strings.TrimSpace(text)
	if text == "" && (len(images) > 0 || len(fileShares) > 0) {
		// chat.postMessage rejects empty text, and the message is still needed
		// as the thread anchor + mirror-link row for an attachment-only comment.
		text = "📎 shared from Linear"
	}

	ts, err := e.slack.PostMessage(ctx, token, slackapi.PostOptions{
		ChannelID: channelID,
		Text:      text,
		Username:  username,
		IconURL:   iconURL,
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
	e.shareFiles(ctx, orgID, d.ID, token, channelID, fileThread, fileShares)

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
	d := p.Linear.Data
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

	token, err := e.intg.SlackBotToken(ctx, orgID)
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
		if err := e.slack.UpdateMessage(ctx, token, slackapi.UpdateOptions{
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
	e.shareFiles(ctx, orgID, d.ID, token, link.SlackChannelID, firstNonEmpty(link.RootSlackTS, link.SlackTS), newFiles)
	return nil
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

// handleNotifBuddy classifies a comment body and, on a create/close command,
// performs it. Returns true if the body was a command (mirroring should stop).
// Commands stay best-effort: failures are logged, never retried via
// redelivery — re-running the classifier on a redelivered comment could
// re-execute a command the user already saw take effect.
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
			slog.ErrorContext(ctx, "sync: notifbuddy create: fetch issue failed", "org_id", orgID, "issue_id", issueID, "error", err)
			return true
		}
		settings, ok := e.settingForTeam(ctx, orgID, issue.TeamID)
		if !ok {
			return true // no config applies to this issue's team
		}
		evt := template.Event{EventType: "linear", Linear: envelopeLinearRaw(raw)}
		if _, err := e.store.ChannelForIssue(ctx, orgID, issueID); err != nil {
			if err := e.ensureChannel(ctx, orgID, issueID, settings, evt, "notifbuddy"); err != nil {
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
