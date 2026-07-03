package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrTeamAlreadyMapped is returned by SaveLinearSetting when the team a caller
// tried to assign is already used by a different config in the same org. It
// carries the offending team id so the service can build a friendly message.
type ErrTeamAlreadyMapped struct {
	TeamID string
}

func (e ErrTeamAlreadyMapped) Error() string {
	return fmt.Sprintf("store: team %s is already used by another linear config", e.TeamID)
}

// LinearSettings is one Linear → Slack channel-creation config, scoped to a
// single Linear team (TeamID). A team belongs to at most one config; the team
// is the config's identity (there is no separate name).
type LinearSettings struct {
	SettingID      string
	OrgID          string
	TeamID         string   // the single Linear team this config applies to
	CreationMode   string   // "status" | "manual" | "condition"
	TriggerStatus  string   // workflow state name that triggers creation (status mode)
	NameTemplate   string   // GHA-expression channel-name template
	ConditionExpr  string   // GHA-expression that must be true to create
	AutoAddMembers []string // Slack member ids (bots + people) to add on creation
}

const linearSettingsCols = `setting_id, org_id, team_id, creation_mode,
	trigger_status, name_template, condition_expr, auto_add_members`

// ListLinearSettings returns all of an org's configs, ordered by name. Returns
// an empty slice (not ErrNotFound) when the org has none.
func (s *Store) ListLinearSettings(ctx context.Context, orgID string) ([]LinearSettings, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+linearSettingsCols+`
		FROM linear_settings
		WHERE org_id = $1
		ORDER BY team_id, setting_id
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list linear settings: %w", err)
	}
	defer rows.Close()

	var out []LinearSettings
	for rows.Next() {
		cfg, err := scanLinearSettings(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, rows.Err()
}

// GetLinearSettingByID returns one config by id (scoped to the org), or
// ErrNotFound.
func (s *Store) GetLinearSettingByID(ctx context.Context, orgID, settingID string) (*LinearSettings, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+linearSettingsCols+`
		FROM linear_settings
		WHERE org_id = $1 AND setting_id = $2
	`, orgID, settingID)
	cfg, err := scanLinearSettings(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SettingForTeam returns the config that applies to a Linear team, or
// ErrNotFound when the team isn't assigned to any config. This is the sync
// engine's resolver: an unmapped team means "do nothing".
func (s *Store) SettingForTeam(ctx context.Context, orgID, teamID string) (*LinearSettings, error) {
	if teamID == "" {
		return nil, ErrNotFound
	}
	row := s.pool.QueryRow(ctx, `
		SELECT `+linearSettingsCols+`
		FROM linear_settings
		WHERE org_id = $1 AND team_id = $2
	`, orgID, teamID)
	cfg, err := scanLinearSettings(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// scanLinearSettings scans a settings row (row or rows) including the jsonb
// auto-add members array.
func scanLinearSettings(sc interface {
	Scan(dest ...any) error
}) (LinearSettings, error) {
	var out LinearSettings
	var membersJSON []byte
	if err := sc.Scan(&out.SettingID, &out.OrgID, &out.TeamID, &out.CreationMode,
		&out.TriggerStatus, &out.NameTemplate, &out.ConditionExpr, &membersJSON); err != nil {
		return LinearSettings{}, err
	}
	var err error
	if out.AutoAddMembers, err = decodeStringArray(membersJSON); err != nil {
		return LinearSettings{}, fmt.Errorf("store: unmarshal members: %w", err)
	}
	return out, nil
}

// decodeStringArray unmarshals a jsonb string array, returning a non-nil empty
// slice when the column is empty/null.
func decodeStringArray(raw []byte) ([]string, error) {
	out := []string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// marshalStringArray marshals a string slice to jsonb, treating nil as [].
func marshalStringArray(in []string) ([]byte, error) {
	if in == nil {
		in = []string{}
	}
	return json.Marshal(in)
}

// SaveLinearSetting inserts or updates one config. When in.SettingID is empty a
// new config is created (its generated id is returned); otherwise the existing
// config (scoped to org) is updated. Assigning a team already owned by another
// config fails with ErrTeamAlreadyMapped{TeamID} (the unique index on
// (org_id, team_id) is the source of truth).
func (s *Store) SaveLinearSetting(ctx context.Context, in LinearSettings) (settingID string, err error) {
	membersJSON, err := marshalStringArray(in.AutoAddMembers)
	if err != nil {
		return "", fmt.Errorf("store: marshal members: %w", err)
	}

	settingID = in.SettingID
	if settingID == "" {
		err = s.pool.QueryRow(ctx, `
			INSERT INTO linear_settings
				(org_id, team_id, creation_mode, trigger_status, name_template, condition_expr, auto_add_members)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING setting_id
		`, in.OrgID, in.TeamID, in.CreationMode, in.TriggerStatus,
			in.NameTemplate, in.ConditionExpr, membersJSON).Scan(&settingID)
		if err != nil {
			return "", wrapTeamConflict(err, in.TeamID, "insert linear setting")
		}
		return settingID, nil
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE linear_settings SET
			team_id          = $3,
			creation_mode    = $4,
			trigger_status   = $5,
			name_template    = $6,
			condition_expr   = $7,
			auto_add_members = $8,
			updated_at       = now()
		WHERE setting_id = $1 AND org_id = $2
	`, settingID, in.OrgID, in.TeamID, in.CreationMode, in.TriggerStatus,
		in.NameTemplate, in.ConditionExpr, membersJSON)
	if err != nil {
		return "", wrapTeamConflict(err, in.TeamID, "update linear setting")
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return settingID, nil
}

// wrapTeamConflict turns a unique-violation on the (org_id, team_id) index into
// the typed ErrTeamAlreadyMapped; any other error is wrapped with context.
func wrapTeamConflict(err error, teamID, op string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return ErrTeamAlreadyMapped{TeamID: teamID}
	}
	return fmt.Errorf("store: %s: %w", op, err)
}

// DeleteLinearSetting removes a config, scoped to the org. Deleting a missing
// config is not an error.
func (s *Store) DeleteLinearSetting(ctx context.Context, orgID, settingID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM linear_settings WHERE setting_id = $1 AND org_id = $2`, settingID, orgID)
	if err != nil {
		return fmt.Errorf("store: delete linear setting: %w", err)
	}
	return nil
}
