package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Provider identifies a third-party integration.
type Provider string

const (
	ProviderSlack  Provider = "slack"
	ProviderLinear Provider = "linear"
)

// Level distinguishes a workspace-wide connection (org install / bot token) from
// a per-user connection (a user's own OAuth token, used to act as that user).
type Level string

const (
	LevelWorkspace Level = "workspace"
	LevelUser      Level = "user"
)

// Norm returns the level defaulted to workspace when empty, so callers that
// don't care about levels keep working unchanged.
func (l Level) Norm() Level {
	if l == "" {
		return LevelWorkspace
	}
	return l
}

// ErrNotFound is returned when an integration row does not exist.
var ErrNotFound = errors.New("store: integration not found")

// Integration is one connected integration for an organization. EncryptedToken
// is ciphertext (or nil); callers decrypt it via crypto.Encryptor. Metadata
// holds provider-specific display data (account login, team name, scopes).
type Integration struct {
	OrgID           string
	Provider        Provider
	Level           Level  // "" is treated as workspace
	ConnectedUserID string // the user this row belongs to (level=user); "" for workspace
	ExternalID      string
	EncryptedToken  []byte
	Metadata        map[string]any
	ConnectedBy     string
}

// UpsertIntegration inserts or replaces the integration for (org, provider).
// Re-connecting overwrites the prior installation/token.
func (s *Store) UpsertIntegration(ctx context.Context, in Integration) error {
	meta := in.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("store: marshal metadata: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO org_integrations
			(org_id, provider, level, connected_user_id, external_id, encrypted_token, metadata, connected_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (org_id, provider, level, connected_user_id) DO UPDATE SET
			external_id     = EXCLUDED.external_id,
			encrypted_token = EXCLUDED.encrypted_token,
			metadata        = EXCLUDED.metadata,
			connected_by    = EXCLUDED.connected_by,
			updated_at      = now()
	`, in.OrgID, string(in.Provider), string(in.Level.Norm()), in.ConnectedUserID,
		in.ExternalID, in.EncryptedToken, metaJSON, in.ConnectedBy)
	if err != nil {
		return fmt.Errorf("store: upsert integration: %w", err)
	}
	return nil
}

// GetIntegration returns the integration for (org, provider, level, userID), or
// ErrNotFound. Use level=LevelWorkspace with userID="" for the workspace row.
func (s *Store) GetIntegration(ctx context.Context, orgID string, provider Provider, level Level, userID string) (*Integration, error) {
	level = level.Norm()
	row := s.pool.QueryRow(ctx, `
		SELECT external_id, encrypted_token, metadata, connected_by
		FROM org_integrations
		WHERE org_id = $1 AND provider = $2 AND level = $3 AND connected_user_id = $4
	`, orgID, string(provider), string(level), userID)

	in := Integration{OrgID: orgID, Provider: provider, Level: level, ConnectedUserID: userID}
	var metaJSON []byte
	var connectedBy *string
	if err := row.Scan(&in.ExternalID, &in.EncryptedToken, &metaJSON, &connectedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get integration: %w", err)
	}
	if connectedBy != nil {
		in.ConnectedBy = *connectedBy
	}
	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &in.Metadata); err != nil {
			return nil, fmt.Errorf("store: unmarshal metadata: %w", err)
		}
	}
	return &in, nil
}

// UserIDBySlackUserID resolves the backend user whose user-level Slack
// connection belongs to the given Slack user id (stored in the row's metadata
// at connect time). ErrNotFound when nobody in the org has linked that Slack
// account.
func (s *Store) UserIDBySlackUserID(ctx context.Context, orgID, slackUserID string) (string, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT connected_user_id
		FROM org_integrations
		WHERE org_id = $1 AND provider = $2 AND level = $3
		  AND metadata->>'slack_user_id' = $4
		LIMIT 1
	`, orgID, string(ProviderSlack), string(LevelUser), slackUserID)
	var userID string
	if err := row.Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("store: user by slack user id: %w", err)
	}
	return userID, nil
}

// ListIntegrations returns an organization's workspace-level integrations.
func (s *Store) ListIntegrations(ctx context.Context, orgID string) ([]Integration, error) {
	return s.listIntegrations(ctx, orgID, LevelWorkspace, "")
}

// ListUserIntegrations returns one user's user-level integrations for an org.
func (s *Store) ListUserIntegrations(ctx context.Context, orgID, userID string) ([]Integration, error) {
	return s.listIntegrations(ctx, orgID, LevelUser, userID)
}

// listIntegrations is the shared query behind List/ListUser, filtering by level
// (and connected_user_id for user rows).
func (s *Store) listIntegrations(ctx context.Context, orgID string, level Level, userID string) ([]Integration, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT provider, external_id, encrypted_token, metadata, connected_by
		FROM org_integrations
		WHERE org_id = $1 AND level = $2 AND connected_user_id = $3
		ORDER BY provider
	`, orgID, string(level.Norm()), userID)
	if err != nil {
		return nil, fmt.Errorf("store: list integrations: %w", err)
	}
	defer rows.Close()

	var out []Integration
	for rows.Next() {
		in := Integration{OrgID: orgID, Level: level.Norm(), ConnectedUserID: userID}
		var provider string
		var metaJSON []byte
		var connectedBy *string
		if err := rows.Scan(&provider, &in.ExternalID, &in.EncryptedToken, &metaJSON, &connectedBy); err != nil {
			return nil, fmt.Errorf("store: scan integration: %w", err)
		}
		in.Provider = Provider(provider)
		if connectedBy != nil {
			in.ConnectedBy = *connectedBy
		}
		if len(metaJSON) > 0 {
			if err := json.Unmarshal(metaJSON, &in.Metadata); err != nil {
				return nil, fmt.Errorf("store: unmarshal metadata: %w", err)
			}
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// DeleteIntegration removes the integration for (org, provider, level, userID).
// Deleting a non-existent row is not an error. Use level=LevelWorkspace,
// userID="" for the workspace row; level=LevelUser, userID=<uid> for a user row.
func (s *Store) DeleteIntegration(ctx context.Context, orgID string, provider Provider, level Level, userID string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM org_integrations
		WHERE org_id = $1 AND provider = $2 AND level = $3 AND connected_user_id = $4
	`, orgID, string(provider), string(level.Norm()), userID)
	if err != nil {
		return fmt.Errorf("store: delete integration: %w", err)
	}
	return nil
}
