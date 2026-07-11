package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// GitHubWebhookEvent is one stored GitHub webhook delivery.
type GitHubWebhookEvent struct {
	ID             int64
	DeliveryID     string
	EventType      string
	InstallationID string
	OrgID          string
	Action         string
	Payload        json.RawMessage
	ReceivedAt     string
}

// InsertGitHubWebhookEvent stores a received webhook, idempotent on delivery
// id. It reports whether this call inserted the row (false on a GitHub
// redelivery) and whether the processed-topic envelope was already published,
// so the writer can retry a publish that failed after the insert committed.
func (s *Store) InsertGitHubWebhookEvent(ctx context.Context, e GitHubWebhookEvent) (inserted, published bool, err error) {
	// The no-op DO UPDATE makes RETURNING yield a row on conflict too;
	// xmax = 0 distinguishes a fresh insert from an existing row.
	err = s.pool.QueryRow(ctx, `
		INSERT INTO github_webhook_events
			(delivery_id, event_type, installation_id, org_id, action, payload)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), $6)
		ON CONFLICT (delivery_id) DO UPDATE SET delivery_id = EXCLUDED.delivery_id
		RETURNING (xmax = 0), envelope_published
	`, e.DeliveryID, e.EventType, e.InstallationID, e.OrgID, e.Action, []byte(e.Payload)).Scan(&inserted, &published)
	if err != nil {
		return false, false, fmt.Errorf("store: insert webhook event: %w", err)
	}
	return inserted, published, nil
}

// MarkGitHubWebhookPublished records that the processed-topic envelope for a
// delivery has been published.
func (s *Store) MarkGitHubWebhookPublished(ctx context.Context, deliveryID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE github_webhook_events SET envelope_published = true WHERE delivery_id = $1
	`, deliveryID)
	if err != nil {
		return fmt.Errorf("store: mark webhook published: %w", err)
	}
	return nil
}

// ListGitHubWebhookEvents returns an org's most recent webhook events, newest
// first, capped at limit.
func (s *Store) ListGitHubWebhookEvents(ctx context.Context, orgID string, limit int) ([]GitHubWebhookEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, delivery_id, event_type, coalesce(installation_id,''),
		       coalesce(org_id,''), coalesce(action,''), payload, received_at
		FROM github_webhook_events
		WHERE org_id = $1
		ORDER BY received_at DESC, id DESC
		LIMIT $2
	`, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list webhook events: %w", err)
	}
	defer rows.Close()

	var out []GitHubWebhookEvent
	for rows.Next() {
		var e GitHubWebhookEvent
		var payload []byte
		var receivedAt time.Time
		if err := rows.Scan(&e.ID, &e.DeliveryID, &e.EventType, &e.InstallationID,
			&e.OrgID, &e.Action, &payload, &receivedAt); err != nil {
			return nil, fmt.Errorf("store: scan webhook event: %w", err)
		}
		e.Payload = json.RawMessage(payload)
		e.ReceivedAt = receivedAt.UTC().Format(time.RFC3339)
		out = append(out, e)
	}
	return out, rows.Err()
}

// OrgIDByGitHubInstallation resolves the org that owns a GitHub installation id,
// or "" + ErrNotFound if no integration matches.
func (s *Store) OrgIDByGitHubInstallation(ctx context.Context, installationID string) (string, error) {
	var orgID string
	err := s.pool.QueryRow(ctx, `
		SELECT org_id FROM org_integrations
		WHERE provider = 'github' AND external_id = $1
		LIMIT 1
	`, installationID).Scan(&orgID)
	if err != nil {
		return "", ErrNotFound
	}
	return orgID, nil
}

// LinearWebhookEvent is one stored Linear webhook delivery.
type LinearWebhookEvent struct {
	ID          int64
	DeliveryID  string
	EventType   string
	WorkspaceID string
	OrgID       string
	Action      string
	Payload     json.RawMessage
	ReceivedAt  string
}

// InsertLinearWebhookEvent stores a received Linear webhook, idempotent on
// delivery id. It reports whether this call inserted the row (false on a
// redelivery) and whether the processed-topic envelope was already published,
// so the writer can retry a publish that failed after the insert committed.
func (s *Store) InsertLinearWebhookEvent(ctx context.Context, e LinearWebhookEvent) (inserted, published bool, err error) {
	err = s.pool.QueryRow(ctx, `
		INSERT INTO linear_webhook_events
			(delivery_id, event_type, workspace_id, org_id, action, payload)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), $6)
		ON CONFLICT (delivery_id) DO UPDATE SET delivery_id = EXCLUDED.delivery_id
		RETURNING (xmax = 0), envelope_published
	`, e.DeliveryID, e.EventType, e.WorkspaceID, e.OrgID, e.Action, []byte(e.Payload)).Scan(&inserted, &published)
	if err != nil {
		return false, false, fmt.Errorf("store: insert linear webhook event: %w", err)
	}
	return inserted, published, nil
}

// MarkLinearWebhookPublished records that the processed-topic envelope for a
// delivery has been published.
func (s *Store) MarkLinearWebhookPublished(ctx context.Context, deliveryID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE linear_webhook_events SET envelope_published = true WHERE delivery_id = $1
	`, deliveryID)
	if err != nil {
		return fmt.Errorf("store: mark linear webhook published: %w", err)
	}
	return nil
}

// ListLinearWebhookEvents returns an org's most recent Linear webhook events,
// newest first, capped at limit.
func (s *Store) ListLinearWebhookEvents(ctx context.Context, orgID string, limit int) ([]LinearWebhookEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, delivery_id, event_type, coalesce(workspace_id,''),
		       coalesce(org_id,''), coalesce(action,''), payload, received_at
		FROM linear_webhook_events
		WHERE org_id = $1
		ORDER BY received_at DESC, id DESC
		LIMIT $2
	`, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list linear webhook events: %w", err)
	}
	defer rows.Close()

	var out []LinearWebhookEvent
	for rows.Next() {
		var e LinearWebhookEvent
		var payload []byte
		var receivedAt time.Time
		if err := rows.Scan(&e.ID, &e.DeliveryID, &e.EventType, &e.WorkspaceID,
			&e.OrgID, &e.Action, &payload, &receivedAt); err != nil {
			return nil, fmt.Errorf("store: scan linear webhook event: %w", err)
		}
		e.Payload = json.RawMessage(payload)
		e.ReceivedAt = receivedAt.UTC().Format(time.RFC3339)
		out = append(out, e)
	}
	return out, rows.Err()
}

// LinearWebhookPayload returns the raw stored payload for a Linear delivery id,
// or ErrNotFound. The sync engine uses this to re-read the full event body (the
// published notification carries only routing fields).
func (s *Store) LinearWebhookPayload(ctx context.Context, deliveryID string) (json.RawMessage, error) {
	var payload []byte
	err := s.pool.QueryRow(ctx, `
		SELECT payload FROM linear_webhook_events WHERE delivery_id = $1
	`, deliveryID).Scan(&payload)
	if err != nil {
		return nil, ErrNotFound
	}
	return json.RawMessage(payload), nil
}

// SlackWebhookPayload returns the raw stored payload for a Slack event id, or
// ErrNotFound. The sync engine uses this to re-read the full event body.
func (s *Store) SlackWebhookPayload(ctx context.Context, eventID string) (json.RawMessage, error) {
	var payload []byte
	err := s.pool.QueryRow(ctx, `
		SELECT payload FROM slack_webhook_events WHERE event_id = $1
	`, eventID).Scan(&payload)
	if err != nil {
		return nil, ErrNotFound
	}
	return json.RawMessage(payload), nil
}

// OrgIDByLinearWorkspace resolves the org that owns a Linear workspace id, or
// "" + ErrNotFound if no integration matches.
func (s *Store) OrgIDByLinearWorkspace(ctx context.Context, workspaceID string) (string, error) {
	var orgID string
	err := s.pool.QueryRow(ctx, `
		SELECT org_id FROM org_integrations
		WHERE provider = 'linear' AND external_id = $1
		LIMIT 1
	`, workspaceID).Scan(&orgID)
	if err != nil {
		return "", ErrNotFound
	}
	return orgID, nil
}

// SlackWebhookEvent is one stored Slack Events API delivery.
type SlackWebhookEvent struct {
	ID         int64
	EventID    string
	EventType  string
	TeamID     string
	OrgID      string
	ChannelID  string
	Payload    json.RawMessage
	ReceivedAt string
}

// InsertSlackWebhookEvent stores a received Slack event, idempotent on Slack's
// event id. It reports whether this call inserted the row (false on a Slack
// retry) and whether the processed-topic envelope was already published, so
// the writer can retry a publish that failed after the insert committed.
func (s *Store) InsertSlackWebhookEvent(ctx context.Context, e SlackWebhookEvent) (inserted, published bool, err error) {
	err = s.pool.QueryRow(ctx, `
		INSERT INTO slack_webhook_events
			(event_id, event_type, team_id, org_id, channel_id, payload)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), $6)
		ON CONFLICT (event_id) DO UPDATE SET event_id = EXCLUDED.event_id
		RETURNING (xmax = 0), envelope_published
	`, e.EventID, e.EventType, e.TeamID, e.OrgID, e.ChannelID, []byte(e.Payload)).Scan(&inserted, &published)
	if err != nil {
		return false, false, fmt.Errorf("store: insert slack webhook event: %w", err)
	}
	return inserted, published, nil
}

// MarkSlackWebhookPublished records that the processed-topic envelope for an
// event has been published.
func (s *Store) MarkSlackWebhookPublished(ctx context.Context, eventID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE slack_webhook_events SET envelope_published = true WHERE event_id = $1
	`, eventID)
	if err != nil {
		return fmt.Errorf("store: mark slack webhook published: %w", err)
	}
	return nil
}

// ListSlackWebhookEvents returns an org's most recent Slack webhook events,
// newest first, capped at limit.
func (s *Store) ListSlackWebhookEvents(ctx context.Context, orgID string, limit int) ([]SlackWebhookEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, event_id, event_type, coalesce(team_id,''),
		       coalesce(org_id,''), coalesce(channel_id,''), payload, received_at
		FROM slack_webhook_events
		WHERE org_id = $1
		ORDER BY received_at DESC, id DESC
		LIMIT $2
	`, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list slack webhook events: %w", err)
	}
	defer rows.Close()

	var out []SlackWebhookEvent
	for rows.Next() {
		var e SlackWebhookEvent
		var payload []byte
		var receivedAt time.Time
		if err := rows.Scan(&e.ID, &e.EventID, &e.EventType, &e.TeamID,
			&e.OrgID, &e.ChannelID, &payload, &receivedAt); err != nil {
			return nil, fmt.Errorf("store: scan slack webhook event: %w", err)
		}
		e.Payload = json.RawMessage(payload)
		e.ReceivedAt = receivedAt.UTC().Format(time.RFC3339)
		out = append(out, e)
	}
	return out, rows.Err()
}

// OrgIDBySlackTeam resolves the org that owns a Slack team (workspace) id, or
// "" + ErrNotFound if no integration matches.
func (s *Store) OrgIDBySlackTeam(ctx context.Context, teamID string) (string, error) {
	var orgID string
	err := s.pool.QueryRow(ctx, `
		SELECT org_id FROM org_integrations
		WHERE provider = 'slack' AND level = 'workspace' AND external_id = $1
		LIMIT 1
	`, teamID).Scan(&orgID)
	if err != nil {
		return "", ErrNotFound
	}
	return orgID, nil
}
