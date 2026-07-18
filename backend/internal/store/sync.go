package store

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5"
)

// LockIssue takes a session-level Postgres advisory lock scoped to (org, issue)
// so concurrent deliveries of the same issue serialize. The sync engine's
// check-then-create-channel path must not run twice — Pub/Sub push is
// at-least-once and concurrent, so two deliveries of one issue event could both
// see "no channel" and both create a Slack channel. It holds a pooled
// connection until the returned release runs, so scope it tightly and always
// call release. Different issues hash to different keys and do not block.
func (s *Store) LockIssue(ctx context.Context, orgID, issueID string) (func(), error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: lock issue acquire: %w", err)
	}
	k1, k2 := int32(fnvHash(orgID)), int32(fnvHash(issueID))
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1, $2)`, k1, k2); err != nil {
		conn.Release()
		return nil, fmt.Errorf("store: lock issue: %w", err)
	}
	return func() {
		// Release on a fresh context so a cancelled request still frees the lock,
		// then return the connection to the pool.
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1, $2)`, k1, k2)
		conn.Release()
	}, nil
}

func fnvHash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

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

// MirroredAsset is one attachment of a mirrored message that has been synced
// to the other side. Inline marks images rendered inside the message's blocks
// (via the asset proxy); false means the file was shared into the thread.
type MirroredAsset struct {
	AssetURL string
	Filename string
	Inline   bool
}

// RecordMirroredAsset marks one of a mirrored object's attachments as synced.
// source names the originating system in envelope vocabulary ("linear", ...)
// and sourceID is that system's id for the containing object (comment id).
// Written right after the successful sync; idempotent so a redelivered update
// can't double-record.
func (s *Store) RecordMirroredAsset(ctx context.Context, orgID, source, sourceID string, a MirroredAsset) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mirrored_assets (org_id, event_source, event_source_id, asset_url, inline, filename)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING
	`, orgID, source, sourceID, a.AssetURL, a.Inline, a.Filename)
	if err != nil {
		return fmt.Errorf("store: record mirrored asset: %w", err)
	}
	return nil
}

// MirroredAssets returns an object's synced attachments in sync order (empty
// when none).
func (s *Store) MirroredAssets(ctx context.Context, orgID, source, sourceID string) ([]MirroredAsset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT asset_url, inline, filename FROM mirrored_assets
		WHERE org_id = $1 AND event_source = $2 AND event_source_id = $3
		ORDER BY created_at, asset_url
	`, orgID, source, sourceID)
	if err != nil {
		return nil, fmt.Errorf("store: mirrored assets: %w", err)
	}
	defer rows.Close()
	var out []MirroredAsset
	for rows.Next() {
		var a MirroredAsset
		if err := rows.Scan(&a.AssetURL, &a.Inline, &a.Filename); err != nil {
			return nil, fmt.Errorf("store: mirrored assets scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
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
