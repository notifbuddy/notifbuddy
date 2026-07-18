// Package sync is the bidirectional Slack <-> Linear sync engine. It subscribes
// to the two ingestion topics (integrations.linear.webhook_event,
// integrations.slack.webhook_event), decides what to mirror, performs the
// Slack/Linear actions, and fires a processing topic per action.
//
// Loop prevention is Defense 1: every message we write is authored by our
// bot/app (Slack posts as the bot with a per-message name/avatar override;
// Linear comments are created with actor=app + createAsUser). So the echo of
// our own write arrives tagged as bot/app-authored, and the engine drops it
// before mirroring it back. The routing tables (issue_channels,
// mirrored_messages) are used only to place messages and resolve thread
// parents — they are not part of loop prevention.
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
}

// Integrations is the subset of integrations.Service the engine needs: token
// access, Linear mutations, and Linear settings. Declared as an interface so
// the engine can be unit-tested without the real service. integrations.Service
// satisfies it.
type Integrations interface {
	SlackBotToken(ctx context.Context, orgID string) (string, error)
	LinearCreateComment(ctx context.Context, orgID string, in integrations.LinearCreateCommentInput) (integrations.LinearComment, error)
	LinearIssueByID(ctx context.Context, orgID, issueID string) (integrations.LinearIssue, error)
	// LinearFileDownload fetches a private Linear upload (uploads.linear.app)
	// with the org's workspace token, for re-hosting in Slack.
	LinearFileDownload(ctx context.Context, orgID, fileURL string) (data []byte, contentType string, err error)
	// LinearAssetProxyURL builds the signed public URL our backend serves a
	// private Linear upload from, for Slack image blocks.
	LinearAssetProxyURL(orgID, fileURL string) (string, error)
	// SettingForTeam resolves the config that applies to a Linear team, or
	// store.ErrNotFound when the team isn't mapped to any config (→ do nothing).
	SettingForTeam(ctx context.Context, orgID, teamID string) (integrations.LinearSettings, error)
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

	// PatchLinearTeamState applies a single WorkflowState webhook to a team's
	// synced status snapshot (upsert, or remove when removed=true).
	PatchLinearTeamState(ctx context.Context, orgID, teamID string, st store.LinearWorkflowState, removed bool) error
}

// LockedCheck reports whether an org's billing has features locked (trial
// expired, no subscription). The sync engine drops inbound events for locked
// orgs — this is the product's valuable path. nil means never locked.
type LockedCheck func(ctx context.Context, orgID string) bool

// Engine wires the stores, Slack/Linear action clients, the intent classifier
// (for @notifbuddy commands), the template engine (channel naming/conditions),
// and the publisher (processing topics). It is safe for concurrent use.
type Engine struct {
	store      Store
	slack      SlackActions
	intg       Integrations
	classifier intent.Classifier
	tmpl       template.Engine
	pub        pubsub.Publisher
	locked     LockedCheck
}

// New builds the engine. pub may be nil (pubsub.Nop is used); the classifier may
// be nil (@notifbuddy commands then resolve to no-action); locked may be nil
// (no billing enforcement).
func New(st Store, slack SlackActions, intg Integrations, classifier intent.Classifier, pub pubsub.Publisher, locked LockedCheck) *Engine {
	if pub == nil {
		pub = pubsub.Nop
	}
	return &Engine{
		store:      st,
		slack:      slack,
		intg:       intg,
		classifier: classifier,
		tmpl:       template.New(),
		pub:        pub,
		locked:     locked,
	}
}

// orgLocked reports whether billing enforcement should drop this org's events.
func (e *Engine) orgLocked(ctx context.Context, orgID string) bool {
	return e.locked != nil && e.locked(ctx, orgID)
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
