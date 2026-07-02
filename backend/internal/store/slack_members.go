package store

import (
	"context"
	"fmt"
	"time"
)

// SlackMember is a synced Slack workspace member (bot/app or human), used to
// populate the auto-add pickers in the Linear channel-rule settings.
type SlackMember struct {
	MemberID string // Slack U… id (what conversations.invite needs)
	Name     string // handle / short name
	RealName string // display / real name
	IconURL  string
	IsBot    bool
	SyncedAt string // RFC3339
}

// GetSlackMembers returns an org's synced Slack members, bots first then humans,
// each alphabetical by display name. Empty slice when nothing synced yet.
func (s *Store) GetSlackMembers(ctx context.Context, orgID string) ([]SlackMember, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT member_id, name, real_name, icon_url, is_bot, synced_at
		FROM slack_members
		WHERE org_id = $1
		ORDER BY is_bot DESC, COALESCE(NULLIF(real_name, ''), name), member_id
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: get slack members: %w", err)
	}
	defer rows.Close()

	var out []SlackMember
	for rows.Next() {
		var m SlackMember
		var syncedAt time.Time
		if err := rows.Scan(&m.MemberID, &m.Name, &m.RealName, &m.IconURL, &m.IsBot, &syncedAt); err != nil {
			return nil, fmt.Errorf("store: scan slack member: %w", err)
		}
		m.SyncedAt = syncedAt.UTC().Format(time.RFC3339)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReplaceSlackMembers fully replaces an org's synced Slack members with the given
// set, in one transaction: members present are upserted, members no longer
// present are deleted (removed/deactivated in Slack). This is the on-connect /
// manual full sync — Slack has no member-removal webhook to patch incrementally.
func (s *Store) ReplaceSlackMembers(ctx context.Context, orgID string, members []SlackMember) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	keep := make([]string, 0, len(members))
	for _, m := range members {
		if _, err := tx.Exec(ctx, `
			INSERT INTO slack_members (org_id, member_id, name, real_name, icon_url, is_bot, synced_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			ON CONFLICT (org_id, member_id) DO UPDATE SET
				name      = EXCLUDED.name,
				real_name = EXCLUDED.real_name,
				icon_url  = EXCLUDED.icon_url,
				is_bot    = EXCLUDED.is_bot,
				synced_at = now()
		`, orgID, m.MemberID, m.Name, m.RealName, m.IconURL, m.IsBot); err != nil {
			return fmt.Errorf("store: upsert slack member: %w", err)
		}
		keep = append(keep, m.MemberID)
	}

	// Drop members that vanished from Slack. keep may be empty (none synced), in
	// which case this clears the org's rows — the correct outcome.
	if _, err := tx.Exec(ctx, `
		DELETE FROM slack_members
		WHERE org_id = $1 AND member_id <> ALL($2)
	`, orgID, keep); err != nil {
		return fmt.Errorf("store: prune slack members: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit slack members: %w", err)
	}
	return nil
}
