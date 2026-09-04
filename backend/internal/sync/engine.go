// Package sync is the bidirectional Slack <-> Linear sync engine. It subscribes
// to the two ingestion topics (integrations.linear.webhook_event,
// integrations.slack.webhook_event), decides what to mirror, performs the
// Slack/Linear actions, and fires a processing topic per action.
//
// Loop prevention: Linear→Slack and Slack→Linear require a linked user token;
// unlinked human authors are dropped. Bot-authored Slack echoes drop on bot_id;
// user-token echoes drop via mirrored_messages (LinkBySlackTS). Linear comments
// we create arrive as actor=app and drop on botActor. Routing tables place
// messages and resolve thread parents.
package sync

import (
	"context"
	"encoding/json"
	"log/slog"

	"xolo/backend/internal/integrations"
	"xolo/backend/internal/intent"
	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/slackapi"
	"xolo/backend/internal/store"
	"xolo/backend/internal/template"
)

// SlackActions is the Slack-side surface the engine needs. It is satisfied by
// slackapi.Client, but declared here so the engine can be tested with a fake.
type SlackActions interface {
	CreateChannel(ctx context.Context, token, name string) (string, error)
	SetChannelTopic(ctx context.Context, token, channelID, topic string) error
	ArchiveChannel(ctx context.Context, token, channelID string) error
	DeleteChannel(ctx context.Context, token, channelID string) error
	InviteUsers(ctx context.Context, token, channelID string, userIDs []string) error
	PostMessage(ctx context.Context, token string, opts slackapi.PostOptions) (string, error)
	LookupUserByEmail(ctx context.Context, token, email string) (slackapi.User, error)
	UserByID(ctx context.Context, token, userID string) (slackapi.User, error)
	AuthTestUserID(ctx context.Context, token string) (string, error)
	DownloadFile(ctx context.Context, token, fileURL string) ([]byte, error)
	UploadFile(ctx context.Context, token string, opts slackapi.UploadOptions) error
	UpdateMessage(ctx context.Context, token string, opts slackapi.UpdateOptions) error
	AddReaction(ctx context.Context, token, channelID, ts, name string) error
	RemoveReaction(ctx context.Context, token, channelID, ts, name string) error
}

// Integrations is the subset of integrations.Service the engine needs: token
// access, Linear mutations, and Linear settings. Declared as an interface so
// the engine can be unit-tested without the real service. integrations.Service
// satisfies it.
type Integrations interface {
	SlackBotToken(ctx context.Context, orgID string) (string, error)
	SlackUserToken(ctx context.Context, orgID, userID string) (string, error)
	// ResolveUserIDByLinearUserID maps a Linear user id to a NotifBuddy user.
	ResolveUserIDByLinearUserID(ctx context.Context, orgID, linearUserID string) (string, error)
	// SlackUserIDByUserID returns the Slack U… id stored on a user's Slack link.
	SlackUserIDByUserID(ctx context.Context, orgID, userID string) (string, error)
	LinearCreateComment(ctx context.Context, orgID string, in integrations.LinearCreateCommentInput) (integrations.LinearComment, error)
	LinearCreateReaction(ctx context.Context, orgID, commentID, emoji, slackAuthorID string) (integrations.LinearReactionResult, error)
	LinearDeleteReaction(ctx context.Context, orgID, reactionID, actingUserID string) error
	LinearIssueByID(ctx context.Context, orgID, issueID string) (integrations.LinearIssue, error)
	// LinearIssueInvitees returns creator/assignee/profile-mentioned users for
	// inviting into a newly created Slack channel. extraBodies are additional
	// markdown bodies to scan for profile URLs (e.g. the @notifbuddy comment).
	LinearIssueInvitees(ctx context.Context, orgID, issueID string, extraBodies ...string) ([]integrations.LinearInvitee, error)
	// LinearFileDownload fetches a private Linear upload (uploads.linear.app)
	// with the org's workspace token, for re-hosting in Slack.
	LinearFileDownload(ctx context.Context, orgID, fileURL string) (data []byte, contentType string, err error)
	// LinearAssetProxyURL builds the signed public URL our backend serves a
	// private Linear upload from, for Slack image blocks.
	LinearAssetProxyURL(orgID, fileURL string) (string, error)
	// SettingForTeam resolves the config that applies to a Linear team, or
	// store.ErrNotFound when the team isn't mapped to any config (→ do nothing).
	SettingForTeam(ctx context.Context, orgID, teamID string) (integrations.LinearSettings, error)
	// LinearMentionForSlackUser returns Linear markdown that @mentions the
	// Linear user linked to slackUserID (profile URL). ok=false when that Slack
	// user has no linked Linear identity.
	LinearMentionForSlackUser(ctx context.Context, orgID, slackUserID string) (mention string, ok bool)
}

// Store is the persistence surface the engine needs: reading stored webhook
// payloads and the routing tables (issue↔channel, mirrored messages). The
// concrete *store.Store satisfies it; tests inject a fake. All methods return
// store.ErrNotFound for a missing row.
type Store interface {
	LinearWebhookPayload(ctx context.Context, deliveryID string) (json.RawMessage, error)
	SlackWebhookPayload(ctx context.Context, eventID string) (json.RawMessage, error)

	// LockIssue serializes concurrent processing of the same issue so the
	// check-then-create-channel path can't run twice under at-least-once,
	// concurrent Pub/Sub delivery. The returned func releases the lock.
	LockIssue(ctx context.Context, orgID, issueID string) (func(), error)

	UpsertIssueChannel(ctx context.Context, in store.IssueChannel) error
	ChannelForIssue(ctx context.Context, orgID, linearIssueID string) (string, error)
	IssueChannelForIssue(ctx context.Context, orgID, linearIssueID string) (store.IssueChannel, error)
	SetIssueChannelTopic(ctx context.Context, orgID, linearIssueID, topic string) error
	IssueForChannel(ctx context.Context, orgID, slackChannelID string) (string, error)
	DeleteIssueChannel(ctx context.Context, orgID, linearIssueID string) error

	RecordMirroredMessage(ctx context.Context, m store.MirroredMessage) error
	LinkBySlackTS(ctx context.Context, orgID, channelID, ts string) (store.MirroredMessage, error)
	LinkByLinearComment(ctx context.Context, orgID, commentID string) (store.MirroredMessage, error)

	// Mirrored assets track which of a mirrored object's attachments were
	// already synced to the other side, keyed by (event_source, id in that
	// system) — today "linear" + comment id (Linear attaches files via a
	// post-create update).
	RecordMirroredAsset(ctx context.Context, orgID, source, sourceID string, a store.MirroredAsset) error
	MirroredAssets(ctx context.Context, orgID, source, sourceID string) ([]store.MirroredAsset, error)

	RecordMirroredReaction(ctx context.Context, orgID string, r store.MirroredReaction) error
	MirroredReactionBySource(ctx context.Context, orgID, eventSource, eventSourceID string) (store.MirroredReaction, error)
	MirroredReactionByCounterpart(ctx context.Context, orgID, counterpartSource, counterpartParentID, counterpartEmoji, counterpartActorID string) (store.MirroredReaction, error)
	DeleteMirroredReaction(ctx context.Context, orgID, eventSource, eventSourceID string) error

	// PatchLinearTeamState applies a single WorkflowState webhook to a team's
	// synced status snapshot (upsert, or remove when removed=true).
	PatchLinearTeamState(ctx context.Context, orgID, teamID string, st store.LinearWorkflowState, removed bool) error
}

// LockedCheck reports whether an org's billing has features locked (trial
// expired, no subscription). The sync engine drops inbound events for locked
// orgs — this is the product's valuable path. nil means never locked.
type LockedCheck func(ctx context.Context, orgID string) bool

type SyncEnabledCheck func(ctx context.Context, orgID string) bool

// Engine wires the stores, Slack/Linear action clients, the intent classifier
// (for @notifbuddy commands), the template engine (channel naming/conditions),
// and the publisher (processing topics). It is safe for concurrent use.
type Engine struct {
	store       Store
	slack       SlackActions
	intg        Integrations
	classifier  intent.Classifier
	tmpl        template.Engine
	pub         pubsub.Publisher
	locked      LockedCheck
	syncEnabled SyncEnabledCheck
}

// New builds the engine. pub may be nil (pubsub.Nop is used); the classifier may
// be nil (@notifbuddy commands then resolve to no-action); locked may be nil
// (no billing enforcement).
func New(st Store, slack SlackActions, intg Integrations, classifier intent.Classifier, pub pubsub.Publisher, locked LockedCheck, syncEnabled SyncEnabledCheck) *Engine {
	if pub == nil {
		pub = pubsub.Nop
	}
	return &Engine{
		store:       st,
		slack:       slack,
		intg:        intg,
		classifier:  classifier,
		tmpl:        template.New(),
		pub:         pub,
		locked:      locked,
		syncEnabled: syncEnabled,
	}
}

// orgLocked reports whether billing enforcement should drop this org's events.
func (e *Engine) orgLocked(ctx context.Context, orgID string) bool {
	return e.locked != nil && e.locked(ctx, orgID)
}

func (e *Engine) orgSyncDisabled(ctx context.Context, orgID string) bool {
	return e.syncEnabled != nil && !e.syncEnabled(ctx, orgID)
}

// publish fires a processing topic best-effort; a failure is logged, never
// surfaced — the action it describes already happened.
func (e *Engine) publish(ctx context.Context, topic string, payload []byte, orgID string) {
	if err := e.pub.Publish(ctx, pubsub.Message{
		Topic:      topic,
		Payload:    payload,
		Attributes: map[string]string{"org_id": orgID},
	}); err != nil {
		slog.ErrorContext(ctx, "sync: publish failed", "topic", topic, "org_id", orgID, "error", err)
	}
}
