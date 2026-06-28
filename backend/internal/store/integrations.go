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
	ProviderGitHub Provider = "github"
	ProviderSlack  Provider = "slack"
	ProviderLinear Provider = "linear"
)

// ErrNotFound is returned when an integration row does not exist.
var ErrNotFound = errors.New("store: integration not found")

// Integration is one connected integration for an organization. EncryptedToken
// is ciphertext (or nil); callers decrypt it via crypto.Encryptor. Metadata
// holds provider-specific display data (account login, team name, scopes).
type Integration struct {
	OrgID          string
	Provider       Provider
	ExternalID     string
	EncryptedToken []byte
	Metadata       map[string]any
	ConnectedBy    string
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
			(org_id, provider, external_id, encrypted_token, metadata, connected_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (org_id, provider) DO UPDATE SET
			external_id     = EXCLUDED.external_id,
			encrypted_token = EXCLUDED.encrypted_token,
			metadata        = EXCLUDED.metadata,
			connected_by    = EXCLUDED.connected_by,
			updated_at      = now()
	`, in.OrgID, string(in.Provider), in.ExternalID, in.EncryptedToken, metaJSON, in.ConnectedBy)
	if err != nil {
		return fmt.Errorf("store: upsert integration: %w", err)
	}
	return nil
}

// GetIntegration returns the integration for (org, provider), or ErrNotFound.
func (s *Store) GetIntegration(ctx context.Context, orgID string, provider Provider) (*Integration, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT external_id, encrypted_token, metadata, connected_by
		FROM org_integrations
		WHERE org_id = $1 AND provider = $2
	`, orgID, string(provider))

	in := Integration{OrgID: orgID, Provider: provider}
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

// ListIntegrations returns all integrations for an organization.
func (s *Store) ListIntegrations(ctx context.Context, orgID string) ([]Integration, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT provider, external_id, encrypted_token, metadata, connected_by
		FROM org_integrations
		WHERE org_id = $1
		ORDER BY provider
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list integrations: %w", err)
	}
	defer rows.Close()

	var out []Integration
	for rows.Next() {
		in := Integration{OrgID: orgID}
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

// DeleteIntegration removes the integration for (org, provider). Deleting a
// non-existent row is not an error.
func (s *Store) DeleteIntegration(ctx context.Context, orgID string, provider Provider) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM org_integrations WHERE org_id = $1 AND provider = $2
	`, orgID, string(provider))
	if err != nil {
		return fmt.Errorf("store: delete integration: %w", err)
	}
	return nil
}
