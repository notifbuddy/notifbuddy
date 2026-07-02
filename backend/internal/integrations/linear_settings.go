package integrations

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"xolo/backend/internal/store"
	"xolo/backend/internal/template"
)

//go:embed sampledata/*.json
var sampleEventsFS embed.FS

// LinearSettings is the service-level view of one Linear channel-rule config,
// scoped to a single Linear team (TeamID). The team is the config's identity.
type LinearSettings struct {
	SettingID      string   `json:"settingId,omitempty"`
	TeamID         string   `json:"teamId"`
	CreationMode   string   `json:"creationMode"`  // "status" | "manual"
	TriggerStatus  string   `json:"triggerStatus"` // workflow state name (status mode)
	NameTemplate   string   `json:"nameTemplate"`
	ConditionExpr  string   `json:"conditionExpr"`
	AutoAddMembers []string `json:"autoAddMembers"` // Slack member ids (bots + people)
}

// LinearSyncReady reports whether the org can actually run the Linear → Slack
// channel sync: it needs BOTH Linear and Slack connected at the workspace level
// (the rules create Slack channels from Linear issues, so Slack is required
// too). The Linear settings UI gates on this.
func (s *Service) LinearSyncReady(ctx context.Context, orgID string) bool {
	return s.connectedAtWorkspace(ctx, orgID, store.ProviderLinear) &&
		s.connectedAtWorkspace(ctx, orgID, store.ProviderSlack)
}

// connectedAtWorkspace reports whether the given provider is connected at the
// workspace level for the org.
func (s *Service) connectedAtWorkspace(ctx context.Context, orgID string, provider store.Provider) bool {
	if !s.Enabled() || orgID == "" {
		return false
	}
	_, err := s.store.GetIntegration(ctx, orgID, provider, store.LevelWorkspace, "")
	return err == nil
}

// ListLinearSettings returns all of an org's named configs (empty slice when
// none saved yet, or when integrations aren't configured).
func (s *Service) ListLinearSettings(ctx context.Context, orgID string) ([]LinearSettings, error) {
	if !s.Enabled() {
		return []LinearSettings{}, nil
	}
	rows, err := s.store.ListLinearSettings(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]LinearSettings, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromStoreLinearSettings(r))
	}
	return out, nil
}

// SettingForTeam returns the config that applies to a Linear team, or
// store.ErrNotFound when the team is unmapped. The sync engine treats
// ErrNotFound as "do nothing" for that team's events.
func (s *Service) SettingForTeam(ctx context.Context, orgID, teamID string) (LinearSettings, error) {
	if !s.Enabled() {
		return LinearSettings{}, store.ErrNotFound
	}
	row, err := s.store.SettingForTeam(ctx, orgID, teamID)
	if err != nil {
		return LinearSettings{}, err
	}
	return fromStoreLinearSettings(*row), nil
}

// SaveLinearSetting validates and persists one config (create when SettingID is
// empty, else update). Templates are parsed up front so a malformed
// template/condition is rejected here rather than failing at channel-creation
// time. A team already owned by another config surfaces as a descriptive error.
// Returns the saved config (with its SettingID).
func (s *Service) SaveLinearSetting(ctx context.Context, orgID string, in LinearSettings) (LinearSettings, error) {
	if !s.Enabled() {
		return LinearSettings{}, fmt.Errorf("integrations: not configured")
	}
	if err := s.validateLinearSettings(in); err != nil {
		return LinearSettings{}, err
	}
	settingID, err := s.store.SaveLinearSetting(ctx, store.LinearSettings{
		SettingID:      in.SettingID,
		OrgID:          orgID,
		TeamID:         in.TeamID,
		CreationMode:   in.CreationMode,
		TriggerStatus:  in.TriggerStatus,
		NameTemplate:   in.NameTemplate,
		ConditionExpr:  in.ConditionExpr,
		AutoAddMembers: orEmptySlice(in.AutoAddMembers),
	})
	if err != nil {
		var mapped store.ErrTeamAlreadyMapped
		if errors.As(err, &mapped) {
			return LinearSettings{}, fmt.Errorf("team is already used by another config")
		}
		return LinearSettings{}, err
	}
	saved, err := s.store.GetLinearSettingByID(ctx, orgID, settingID)
	if err != nil {
		return LinearSettings{}, err
	}
	return fromStoreLinearSettings(*saved), nil
}

// DeleteLinearSetting removes a config (and its team mappings) for the org.
func (s *Service) DeleteLinearSetting(ctx context.Context, orgID, settingID string) error {
	if !s.Enabled() {
		return fmt.Errorf("integrations: not configured")
	}
	return s.store.DeleteLinearSetting(ctx, orgID, settingID)
}

// validateLinearSettings enforces the mode/status/template rules shared by
// create and update.
func (s *Service) validateLinearSettings(in LinearSettings) error {
	if strings.TrimSpace(in.TeamID) == "" {
		return fmt.Errorf("a team is required")
	}
	if in.CreationMode != "status" && in.CreationMode != "manual" {
		return fmt.Errorf("invalid creation mode %q", in.CreationMode)
	}
	if in.CreationMode == "status" && strings.TrimSpace(in.TriggerStatus) == "" {
		return fmt.Errorf("trigger status is required when creation mode is 'status'")
	}
	// Validate templates by rendering/evaluating against an empty event: a parse
	// error surfaces here; a missing-field (null) does not.
	empty := template.Event{EventType: "linear", Linear: map[string]any{}}
	if in.NameTemplate != "" {
		if _, err := s.tmpl.Render(in.NameTemplate, empty); err != nil {
			return fmt.Errorf("name template: %w", err)
		}
	}
	if in.ConditionExpr != "" {
		if _, err := s.tmpl.Evaluate(in.ConditionExpr, empty); err != nil {
			return fmt.Errorf("condition: %w", err)
		}
	}
	return nil
}

// fromStoreLinearSettings maps a store row to the service DTO.
func fromStoreLinearSettings(r store.LinearSettings) LinearSettings {
	return LinearSettings{
		SettingID:      r.SettingID,
		TeamID:         r.TeamID,
		CreationMode:   orDefault(r.CreationMode, "manual"),
		TriggerStatus:  r.TriggerStatus,
		NameTemplate:   r.NameTemplate,
		ConditionExpr:  r.ConditionExpr,
		AutoAddMembers: orEmptySlice(r.AutoAddMembers),
	}
}

// orEmptySlice returns a non-nil empty slice for nil input (so JSON encodes [],
// not null, and DB writes get a concrete array).
func orEmptySlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// --- Team workflow-state sync ------------------------------------------------

// LinearTeamStatesView is a team plus its workflow states, for the settings UI.
type LinearTeamStatesView struct {
	TeamID   string                `json:"teamId"`
	TeamKey  string                `json:"teamKey"`
	TeamName string                `json:"teamName"`
	States   []LinearWorkflowState `json:"states"`
}

// SyncLinearTeamStates fetches every team's workflow states from Linear and
// replaces the org's stored snapshot. Called on connect and on demand.
func (s *Service) SyncLinearTeamStates(ctx context.Context, orgID string) error {
	if !s.Enabled() {
		return fmt.Errorf("integrations: not configured")
	}
	teams, err := s.LinearTeamStates(ctx, orgID)
	if err != nil {
		return err
	}
	rows := make([]store.LinearTeamStates, 0, len(teams))
	for _, t := range teams {
		states := make([]store.LinearWorkflowState, 0, len(t.States))
		for _, st := range t.States {
			states = append(states, store.LinearWorkflowState{
				ID: st.ID, Name: st.Name, Type: st.Type, Color: st.Color, Position: st.Position,
			})
		}
		rows = append(rows, store.LinearTeamStates{
			OrgID: orgID, TeamID: t.TeamID, TeamKey: t.TeamKey, TeamName: t.TeamName, States: states,
		})
	}
	return s.store.ReplaceLinearTeamStates(ctx, orgID, rows)
}

// GetLinearTeamStates returns the org's synced team states for the settings UI
// (empty slice when nothing synced yet).
func (s *Service) GetLinearTeamStates(ctx context.Context, orgID string) ([]LinearTeamStatesView, error) {
	if !s.Enabled() {
		return []LinearTeamStatesView{}, nil
	}
	rows, err := s.store.GetLinearTeamStates(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]LinearTeamStatesView, 0, len(rows))
	for _, r := range rows {
		states := make([]LinearWorkflowState, 0, len(r.States))
		for _, st := range r.States {
			states = append(states, LinearWorkflowState{
				ID: st.ID, Name: st.Name, Type: st.Type, Color: st.Color, Position: st.Position,
			})
		}
		out = append(out, LinearTeamStatesView{
			TeamID: r.TeamID, TeamKey: r.TeamKey, TeamName: r.TeamName, States: states,
		})
	}
	return out, nil
}

// TemplateTestResult is the outcome of testing a name template + condition
// against an event. Err carries the first failure (parse/eval) for display.
type TemplateTestResult struct {
	Name            string `json:"name"`
	ConditionResult bool   `json:"conditionResult"`
	Err             string `json:"error,omitempty"`
}

// TestLinearTemplate renders nameTemplate and evaluates conditionExpr against the
// given event. It is pure (no persistence). Either field may be empty.
func (s *Service) TestLinearTemplate(evt template.Event, nameTemplate, conditionExpr string) TemplateTestResult {
	var res TemplateTestResult
	if nameTemplate != "" {
		name, err := s.tmpl.Render(nameTemplate, evt)
		if err != nil {
			res.Err = "name template: " + err.Error()
			return res
		}
		res.Name = name
	}
	if conditionExpr != "" {
		ok, err := s.tmpl.Evaluate(conditionExpr, evt)
		if err != nil {
			res.Err = "condition: " + err.Error()
			return res
		}
		res.ConditionResult = ok
	} else {
		res.ConditionResult = true // no condition = always passes
	}
	return res
}

// SampleEvent is a built-in example event for the settings test UI.
type SampleEvent struct {
	ID    string `json:"id"`    // file stem, e.g. "issue.status_changed"
	Label string `json:"label"` // human label
	Raw   string `json:"raw"`   // the envelope JSON
}

// ListLinearSampleEvents returns the embedded sample Linear events so a user can
// validate templates without triggering a real Linear event.
func (s *Service) ListLinearSampleEvents() ([]SampleEvent, error) {
	entries, err := fs.ReadDir(sampleEventsFS, "sampledata")
	if err != nil {
		return nil, err
	}
	var out []SampleEvent
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := sampleEventsFS.ReadFile("sampledata/" + e.Name())
		if err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		out = append(out, SampleEvent{ID: id, Label: sampleLabel(id), Raw: string(raw)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// LinearSampleEventRaw returns the raw envelope JSON for a sample event id, or
// an error if the id is unknown.
func (s *Service) LinearSampleEventRaw(id string) ([]byte, error) {
	// id is a file stem (e.g. "issue.status_changed"); reject path separators to
	// avoid traversal. Dots are legitimate in the stem.
	if id == "" || strings.ContainsAny(id, "/\\") {
		return nil, fmt.Errorf("integrations: invalid sample id")
	}
	raw, err := sampleEventsFS.ReadFile("sampledata/" + id + ".json")
	if err != nil {
		return nil, fmt.Errorf("integrations: unknown sample event %q", id)
	}
	return raw, nil
}

// sampleLabel turns a file stem ("issue.status_changed") into a readable label.
func sampleLabel(id string) string {
	id = strings.ReplaceAll(id, "_", " ")
	id = strings.ReplaceAll(id, ".", ": ")
	return id
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
