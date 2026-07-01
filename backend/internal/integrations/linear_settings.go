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

// LinearSettings is the service-level view of an org's Linear channel rules.
type LinearSettings struct {
	CreationMode  string   `json:"creationMode"`  // "status" | "manual"
	TriggerStatus string   `json:"triggerStatus"` // workflow state name (status mode)
	NameTemplate  string   `json:"nameTemplate"`
	ConditionExpr string   `json:"conditionExpr"`
	AutoAddBots   []string `json:"autoAddBots"`
}

// defaultLinearSettings is returned when an org hasn't saved any yet.
func defaultLinearSettings() LinearSettings {
	return LinearSettings{CreationMode: "manual", AutoAddBots: []string{}}
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

// GetLinearSettings returns the org's saved Linear settings, or defaults.
func (s *Service) GetLinearSettings(ctx context.Context, orgID string) (LinearSettings, error) {
	if !s.Enabled() {
		return defaultLinearSettings(), nil
	}
	row, err := s.store.GetLinearSettings(ctx, orgID)
	if errors.Is(err, store.ErrNotFound) {
		return defaultLinearSettings(), nil
	}
	if err != nil {
		return LinearSettings{}, err
	}
	bots := row.AutoAddBots
	if bots == nil {
		bots = []string{}
	}
	return LinearSettings{
		CreationMode:  orDefault(row.CreationMode, "manual"),
		TriggerStatus: row.TriggerStatus,
		NameTemplate:  row.NameTemplate,
		ConditionExpr: row.ConditionExpr,
		AutoAddBots:   bots,
	}, nil
}

// SaveLinearSettings validates and persists the org's Linear settings. Templates
// are validated by parsing them, so a malformed template/condition is rejected
// up front rather than failing silently at channel-creation time.
func (s *Service) SaveLinearSettings(ctx context.Context, orgID string, in LinearSettings) error {
	if !s.Enabled() {
		return fmt.Errorf("integrations: not configured")
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
	bots := in.AutoAddBots
	if bots == nil {
		bots = []string{}
	}
	return s.store.UpsertLinearSettings(ctx, store.LinearSettings{
		OrgID:         orgID,
		CreationMode:  in.CreationMode,
		TriggerStatus: in.TriggerStatus,
		NameTemplate:  in.NameTemplate,
		ConditionExpr: in.ConditionExpr,
		AutoAddBots:   bots,
	})
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
