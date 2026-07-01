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
	"log"

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
}

// Integrations is the subset of integrations.Service the engine needs: token
// access, Linear mutations, and Linear settings. Declared as an interface so
// the engine can be unit-tested without the real service. integrations.Service
// satisfies it.
type Integrations interface {
	SlackBotToken(ctx context.Context, orgID string) (string, error)
	LinearCreateComment(ctx context.Context, orgID string, in integrations.LinearCreateCommentInput) (integrations.LinearComment, error)
	LinearIssueByID(ctx context.Context, orgID, issueID string) (integrations.LinearIssue, error)
	GetLinearSettings(ctx context.Context, orgID string) (integrations.LinearSettings, error)
}

// Store is the persistence surface the engine needs: reading stored webhook
// payloads and the routing tables (issue↔channel, mirrored messages). The
// concrete *store.Store satisfies it; tests inject a fake. All methods return
// store.ErrNotFound for a missing row.
type Store interface {
	LinearWebhookPayload(ctx context.Context, deliveryID string) (json.RawMessage, error)
	SlackWebhookPayload(ctx context.Context, eventID string) (json.RawMessage, error)

	UpsertIssueChannel(ctx context.Context, in store.IssueChannel) error
	ChannelForIssue(ctx context.Context, orgID, linearIssueID string) (string, error)
	IssueForChannel(ctx context.Context, orgID, slackChannelID string) (string, error)
	DeleteIssueChannel(ctx context.Context, orgID, linearIssueID string) error

	RecordMirroredMessage(ctx context.Context, m store.MirroredMessage) error
	LinkBySlackTS(ctx context.Context, orgID, channelID, ts string) (store.MirroredMessage, error)
	LinkByLinearComment(ctx context.Context, orgID, commentID string) (store.MirroredMessage, error)
}

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
}

// New builds the engine. pub may be nil (pubsub.Nop is used); the classifier may
// be nil (@notifbuddy commands then resolve to no-action).
func New(st Store, slack SlackActions, intg Integrations, classifier intent.Classifier, pub pubsub.Publisher) *Engine {
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
	}
}

// publish fires a processing topic best-effort; a failure is logged, never
// surfaced — the action it describes already happened.
func (e *Engine) publish(ctx context.Context, topic string, payload []byte, orgID string) {
	if err := e.pub.Publish(ctx, pubsub.Message{
		Topic:      topic,
		Payload:    payload,
		Attributes: map[string]string{"org_id": orgID},
	}); err != nil {
		log.Printf("sync: publish %s failed: %v", topic, err)
	}
}
