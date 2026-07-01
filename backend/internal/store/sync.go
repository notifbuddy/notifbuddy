package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// IssueChannel is the one Slack channel mapped to a Linear issue for an org.
type IssueChannel struct {
	OrgID          string
	LinearIssueID  string
	SlackChannelID string
}

// UpsertIssueChannel records (or replaces) the channel for a Linear issue. The
// mapping is one-channel-per-issue, so re-creating overwrites the prior row.
func (s *Store) UpsertIssueChannel(ctx context.Context, in IssueChannel) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO issue_channels (org_id, linear_issue_id, slack_channel_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (org_id, linear_issue_id) DO UPDATE SET
			slack_channel_id = EXCLUDED.slack_channel_id,
			created_at       = now()
	`, in.OrgID, in.LinearIssueID, in.SlackChannelID)
	if err != nil {
		return fmt.Errorf("store: upsert issue channel: %w", err)
	}
	return nil
}

// ChannelForIssue returns the Slack channel id mapped to a Linear issue, or
// ErrNotFound.
func (s *Store) ChannelForIssue(ctx context.Context, orgID, linearIssueID string) (string, error) {
	var channelID string
	err := s.pool.QueryRow(ctx, `
		SELECT slack_channel_id FROM issue_channels
		WHERE org_id = $1 AND linear_issue_id = $2
	`, orgID, linearIssueID).Scan(&channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: channel for issue: %w", err)
	}
	return channelID, nil
}

// IssueForChannel returns the Linear issue id mapped to a Slack channel, or
// ErrNotFound. Used to route inbound Slack messages to their issue.
func (s *Store) IssueForChannel(ctx context.Context, orgID, slackChannelID string) (string, error) {
	var issueID string
	err := s.pool.QueryRow(ctx, `
		SELECT linear_issue_id FROM issue_channels
		WHERE org_id = $1 AND slack_channel_id = $2
	`, orgID, slackChannelID).Scan(&issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: issue for channel: %w", err)
	}
	return issueID, nil
}

// DeleteIssueChannel removes the mapping for an issue (on channel close/delete).
func (s *Store) DeleteIssueChannel(ctx context.Context, orgID, linearIssueID string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM issue_channels WHERE org_id = $1 AND linear_issue_id = $2
	`, orgID, linearIssueID)
	if err != nil {
		return fmt.Errorf("store: delete issue channel: %w", err)
	}
	return nil
}

// MirroredMessage links a mirrored comment/message to its counterpart. Root*
// hold the thread root's counterpart ids so a reply is placed under the right
// parent on the other side (equal to the row's own ids for a top-level message).
type MirroredMessage struct {
	OrgID               string
	LinearCommentID     string
	SlackChannelID      string
	SlackTS             string
	RootLinearCommentID string
	RootSlackTS         string
}

// RecordMirroredMessage stores a mirror link. It is written the moment a
// mirrored message is created, so an echo can be recognized and routing/thread
// resolution works. Idempotent on either side's unique key.
func (s *Store) RecordMirroredMessage(ctx context.Context, m MirroredMessage) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mirrored_messages
			(org_id, linear_comment_id, slack_channel_id, slack_ts,
			 root_linear_comment_id, root_slack_ts)
		VALUES ($1, $2, $3, $4, NULLIF($5,''), NULLIF($6,''))
		ON CONFLICT (org_id, linear_comment_id) DO NOTHING
	`, m.OrgID, m.LinearCommentID, m.SlackChannelID, m.SlackTS,
		m.RootLinearCommentID, m.RootSlackTS)
	if err != nil {
		return fmt.Errorf("store: record mirrored message: %w", err)
	}
	return nil
}

// LinkBySlackTS returns the mirror link for a Slack message, or ErrNotFound.
func (s *Store) LinkBySlackTS(ctx context.Context, orgID, channelID, ts string) (MirroredMessage, error) {
	return s.scanLink(ctx, `
		SELECT org_id, linear_comment_id, slack_channel_id, slack_ts,
		       coalesce(root_linear_comment_id,''), coalesce(root_slack_ts,'')
		FROM mirrored_messages
		WHERE org_id = $1 AND slack_channel_id = $2 AND slack_ts = $3
	`, orgID, channelID, ts)
}

// LinkByLinearComment returns the mirror link for a Linear comment, or
// ErrNotFound.
func (s *Store) LinkByLinearComment(ctx context.Context, orgID, commentID string) (MirroredMessage, error) {
	return s.scanLink(ctx, `
		SELECT org_id, linear_comment_id, slack_channel_id, slack_ts,
		       coalesce(root_linear_comment_id,''), coalesce(root_slack_ts,'')
		FROM mirrored_messages
		WHERE org_id = $1 AND linear_comment_id = $2
	`, orgID, commentID)
}

func (s *Store) scanLink(ctx context.Context, query string, args ...any) (MirroredMessage, error) {
	var m MirroredMessage
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&m.OrgID, &m.LinearCommentID, &m.SlackChannelID, &m.SlackTS,
		&m.RootLinearCommentID, &m.RootSlackTS)
	if errors.Is(err, pgx.ErrNoRows) {
		return MirroredMessage{}, ErrNotFound
	}
	if err != nil {
		return MirroredMessage{}, fmt.Errorf("store: scan mirror link: %w", err)
	}
	return m, nil
}
