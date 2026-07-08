package integrations

import (
	"testing"

	"xolo/backend/internal/template"
)

// issueEvent builds an event envelope for an issue in the given workflow
// state, the shape both the engine and the test panel evaluate against.
func issueEvent(stateName, stateType string) template.Event {
	return template.Event{
		EventType: "linear",
		Linear: map[string]any{
			"action": "update",
			"data": map[string]any{
				"identifier": "SKO-9",
				"state":      map[string]any{"name": stateName, "type": stateType},
			},
		},
	}
}

// The trigger rules shared by the sync engine and the settings test panel:
// status mode compares the event's workflow state, condition mode evaluates
// the expression, manual never auto-fires — and the two triggers are
// independent (an issue in Todo creates but must not archive, and vice versa).
func TestLinearSettingsTriggers(t *testing.T) {
	s := newTestService(t)

	// The reported scenario: create on "Todo", archive on "Done".
	statusConfig := LinearSettings{
		CreationMode:  "status",
		TriggerStatus: "Todo",
		ArchiveMode:   "status",
		ArchiveStatus: "Done",
	}
	conditionConfig := LinearSettings{
		CreationMode:         "condition",
		ConditionExpr:        "linear.data.state.name == 'Todo'",
		ArchiveMode:          "condition",
		ArchiveConditionExpr: "linear.data.state.type == 'completed'",
	}
	manualConfig := LinearSettings{CreationMode: "manual", ArchiveMode: "manual"}

	tests := []struct {
		name         string
		config       LinearSettings
		evt          template.Event
		wouldCreate  bool
		wouldArchive bool
	}{
		{"status: Todo creates, does not archive", statusConfig, issueEvent("Todo", "unstarted"), true, false},
		{"status: Done archives, does not create", statusConfig, issueEvent("Done", "completed"), false, true},
		{"status: unrelated state does neither", statusConfig, issueEvent("Backlog", "backlog"), false, false},
		{"status: state match is case-insensitive", statusConfig, issueEvent("todo", "unstarted"), true, false},
		{"status: event without a state does neither", statusConfig, template.Event{EventType: "linear", Linear: map[string]any{"data": map[string]any{}}}, false, false},
		{"condition: create expr true, archive expr false", conditionConfig, issueEvent("Todo", "unstarted"), true, false},
		{"condition: archive expr true, create expr false", conditionConfig, issueEvent("Done", "completed"), false, true},
		{"manual: never auto-fires", manualConfig, issueEvent("Todo", "unstarted"), false, false},
		{"empty modes behave as manual", LinearSettings{}, issueEvent("Todo", "unstarted"), false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := s.TestLinearTemplate(tc.evt, tc.config)
			if res.Err != "" {
				t.Fatalf("unexpected error: %s", res.Err)
			}
			if res.WouldCreate != tc.wouldCreate {
				t.Errorf("wouldCreate = %v, want %v", res.WouldCreate, tc.wouldCreate)
			}
			if res.WouldArchive != tc.wouldArchive {
				t.Errorf("wouldArchive = %v, want %v", res.WouldArchive, tc.wouldArchive)
			}

			// The panel must agree with the engine's shared trigger helpers.
			stateName := EventStateName(tc.evt)
			if got, _ := CreateTriggered(s.tmpl, tc.config, stateName, tc.evt); got != tc.wouldCreate {
				t.Errorf("CreateTriggered = %v, want %v", got, tc.wouldCreate)
			}
			if got, _ := ArchiveTriggered(s.tmpl, tc.config, stateName, tc.evt); got != tc.wouldArchive {
				t.Errorf("ArchiveTriggered = %v, want %v", got, tc.wouldArchive)
			}
		})
	}

	t.Run("name template renders alongside triggers", func(t *testing.T) {
		cfg := statusConfig
		cfg.NameTemplate = "tkt-${{ lowercase(linear.data.identifier) }}"
		res := s.TestLinearTemplate(issueEvent("Todo", "unstarted"), cfg)
		if res.Name != "tkt-sko-9" {
			t.Errorf("name = %q, want tkt-sko-9", res.Name)
		}
	})

	t.Run("bad archive expression surfaces an error", func(t *testing.T) {
		res := s.TestLinearTemplate(issueEvent("Done", "completed"), LinearSettings{
			CreationMode: "manual", ArchiveMode: "condition", ArchiveConditionExpr: "linear.data. ==",
		})
		if res.Err == "" {
			t.Fatal("expected an archive condition error")
		}
	})

	t.Run("status mode with a condition gate requires both", func(t *testing.T) {
		cfg := statusConfig
		cfg.ConditionExpr = "linear.data.state.type == 'completed'" // gate that fails for Todo
		res := s.TestLinearTemplate(issueEvent("Todo", "unstarted"), cfg)
		if res.Err != "" {
			t.Fatalf("unexpected error: %s", res.Err)
		}
		if res.WouldCreate {
			t.Error("status match with a failing condition gate must not create (mirrors ensureChannel)")
		}
	})
}
