package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// LinearWorkflowState is one workflow state (issue status) of a Linear team.
type LinearWorkflowState struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Color    string  `json:"color"`
	Position float64 `json:"position"`
}

// LinearTeamStates is a synced snapshot of one team's workflow states, used to
// populate the trigger-status dropdown.
type LinearTeamStates struct {
	OrgID    string
	TeamID   string
	TeamKey  string
	TeamName string
	States   []LinearWorkflowState
	SyncedAt string // RFC3339
}

// GetLinearTeamStates returns every team's synced states for an org, ordered by
// team name. Empty slice when nothing has been synced yet.
func (s *Store) GetLinearTeamStates(ctx context.Context, orgID string) ([]LinearTeamStates, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT org_id, team_id, team_key, team_name, states, synced_at
		FROM linear_team_states
		WHERE org_id = $1
		ORDER BY team_name, team_id
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: get linear team states: %w", err)
	}
	defer rows.Close()

	var out []LinearTeamStates
	for rows.Next() {
		var t LinearTeamStates
		var statesJSON []byte
		var syncedAt time.Time
		if err := rows.Scan(&t.OrgID, &t.TeamID, &t.TeamKey, &t.TeamName, &statesJSON, &syncedAt); err != nil {
			return nil, fmt.Errorf("store: scan linear team states: %w", err)
		}
		if len(statesJSON) > 0 {
			if err := json.Unmarshal(statesJSON, &t.States); err != nil {
				return nil, fmt.Errorf("store: unmarshal team states: %w", err)
			}
		}
		if t.States == nil {
			t.States = []LinearWorkflowState{}
		}
		t.SyncedAt = syncedAt.UTC().Format(time.RFC3339)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ReplaceLinearTeamStates fully replaces an org's synced team states with the
// given set, in one transaction: teams present are upserted, teams no longer
// present are deleted (they were removed in Linear). This is the on-connect /
// manual full sync.
func (s *Store) ReplaceLinearTeamStates(ctx context.Context, orgID string, teams []LinearTeamStates) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	keep := make([]string, 0, len(teams))
	for _, t := range teams {
		states := t.States
		if states == nil {
			states = []LinearWorkflowState{}
		}
		statesJSON, err := json.Marshal(states)
		if err != nil {
			return fmt.Errorf("store: marshal team states: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO linear_team_states (org_id, team_id, team_key, team_name, states, synced_at)
			VALUES ($1, $2, $3, $4, $5, now())
			ON CONFLICT (org_id, team_id) DO UPDATE SET
				team_key  = EXCLUDED.team_key,
				team_name = EXCLUDED.team_name,
				states    = EXCLUDED.states,
				synced_at = now()
		`, orgID, t.TeamID, t.TeamKey, t.TeamName, statesJSON); err != nil {
			return fmt.Errorf("store: upsert team states: %w", err)
		}
		keep = append(keep, t.TeamID)
	}

	// Drop teams that vanished from Linear. keep may be empty (no teams synced),
	// in which case this deletes all rows for the org — the correct outcome.
	if _, err := tx.Exec(ctx, `
		DELETE FROM linear_team_states
		WHERE org_id = $1 AND team_id <> ALL($2)
	`, orgID, keep); err != nil {
		return fmt.Errorf("store: prune team states: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit team states: %w", err)
	}
	return nil
}

// PatchLinearTeamState applies a single WorkflowState webhook to a team's synced
// snapshot: it upserts (removed=false) or removes (removed=true) the one state
// by id, leaving the rest of the array intact. The team row is created if it
// doesn't exist yet (a webhook can arrive for a team we haven't full-synced).
func (s *Store) PatchLinearTeamState(ctx context.Context, orgID, teamID string, st LinearWorkflowState, removed bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	// Load the current array (locking the row if present).
	var statesJSON []byte
	err = tx.QueryRow(ctx, `
		SELECT states FROM linear_team_states
		WHERE org_id = $1 AND team_id = $2
		FOR UPDATE
	`, orgID, teamID).Scan(&statesJSON)
	var states []LinearWorkflowState
	if err == nil && len(statesJSON) > 0 {
		if err := json.Unmarshal(statesJSON, &states); err != nil {
			return fmt.Errorf("store: unmarshal team states: %w", err)
		}
	}
	// (err == pgx.ErrNoRows → team not synced yet; states stays nil and we insert.)

	// Rebuild the array: drop any existing entry with this id, then re-add unless
	// this is a removal.
	next := states[:0:0]
	for _, existing := range states {
		if existing.ID != st.ID {
			next = append(next, existing)
		}
	}
	if !removed {
		next = append(next, st)
	}
	nextJSON, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("store: marshal team states: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO linear_team_states (org_id, team_id, states, synced_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (org_id, team_id) DO UPDATE SET
			states    = EXCLUDED.states,
			synced_at = now()
	`, orgID, teamID, nextJSON); err != nil {
		return fmt.Errorf("store: patch team state: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit patch team state: %w", err)
	}
	return nil
}
