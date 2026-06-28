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

// InsertGitHubWebhookEvent stores a received webhook. On a duplicate delivery id
// (GitHub redelivery) it does nothing and reports inserted=false, so the caller
// can treat redeliveries idempotently.
func (s *Store) InsertGitHubWebhookEvent(ctx context.Context, e GitHubWebhookEvent) (inserted bool, err error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO github_webhook_events
			(delivery_id, event_type, installation_id, org_id, action, payload)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), $6)
		ON CONFLICT (delivery_id) DO NOTHING
	`, e.DeliveryID, e.EventType, e.InstallationID, e.OrgID, e.Action, []byte(e.Payload))
	if err != nil {
		return false, fmt.Errorf("store: insert webhook event: %w", err)
	}
	return tag.RowsAffected() > 0, nil
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

// InsertLinearWebhookEvent stores a received Linear webhook. On a duplicate
// delivery id (redelivery) it does nothing and reports inserted=false, so the
// caller can treat redeliveries idempotently.
func (s *Store) InsertLinearWebhookEvent(ctx context.Context, e LinearWebhookEvent) (inserted bool, err error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO linear_webhook_events
			(delivery_id, event_type, workspace_id, org_id, action, payload)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), $6)
		ON CONFLICT (delivery_id) DO NOTHING
	`, e.DeliveryID, e.EventType, e.WorkspaceID, e.OrgID, e.Action, []byte(e.Payload))
	if err != nil {
		return false, fmt.Errorf("store: insert linear webhook event: %w", err)
	}
	return tag.RowsAffected() > 0, nil
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
