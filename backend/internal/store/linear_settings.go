package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LinearSettings is an org's Linear → Slack channel-creation configuration.
type LinearSettings struct {
	OrgID         string
	CreationMode  string   // "status" | "manual"
	TriggerStatus string   // workflow state name that triggers creation (status mode)
	NameTemplate  string   // GHA-expression channel-name template
	ConditionExpr string   // GHA-expression that must be true to create
	AutoAddBots   []string // bots to add on creation
}

// GetLinearSettings returns an org's Linear settings, or ErrNotFound if none
// have been saved yet (the caller then treats it as defaults).
func (s *Store) GetLinearSettings(ctx context.Context, orgID string) (*LinearSettings, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT creation_mode, trigger_status, name_template, condition_expr, auto_add_bots
		FROM linear_settings
		WHERE org_id = $1
	`, orgID)

	out := LinearSettings{OrgID: orgID}
	var botsJSON []byte
	if err := row.Scan(&out.CreationMode, &out.TriggerStatus, &out.NameTemplate, &out.ConditionExpr, &botsJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get linear settings: %w", err)
	}
	if len(botsJSON) > 0 {
		if err := json.Unmarshal(botsJSON, &out.AutoAddBots); err != nil {
			return nil, fmt.Errorf("store: unmarshal bots: %w", err)
		}
	}
	return &out, nil
}

// UpsertLinearSettings inserts or replaces an org's Linear settings.
func (s *Store) UpsertLinearSettings(ctx context.Context, in LinearSettings) error {
	bots := in.AutoAddBots
	if bots == nil {
		bots = []string{}
	}
	botsJSON, err := json.Marshal(bots)
	if err != nil {
		return fmt.Errorf("store: marshal bots: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO linear_settings
			(org_id, creation_mode, trigger_status, name_template, condition_expr, auto_add_bots, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (org_id) DO UPDATE SET
			creation_mode  = EXCLUDED.creation_mode,
			trigger_status = EXCLUDED.trigger_status,
			name_template  = EXCLUDED.name_template,
			condition_expr = EXCLUDED.condition_expr,
			auto_add_bots  = EXCLUDED.auto_add_bots,
			updated_at     = now()
	`, in.OrgID, in.CreationMode, in.TriggerStatus, in.NameTemplate, in.ConditionExpr, botsJSON)
	if err != nil {
		return fmt.Errorf("store: upsert linear settings: %w", err)
	}
	return nil
}
