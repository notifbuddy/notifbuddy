//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// TestIntegrationStatus_Unauthenticated asserts the status endpoint needs a
// session.
func TestIntegrationStatus_Unauthenticated(t *testing.T) {
	r := getJSON(t, nil, "/integrations/status")
	requireStatus(t, r, http.StatusUnauthorized)
}

// TestIntegrationStatus_FreshOrg asserts a brand-new org sees the service
// configured (DB is up) with nothing connected yet.
func TestIntegrationStatus_FreshOrg(t *testing.T) {
	s := newSession(t, "user_status", "status@e2e.test", "org_status_fresh", "admin")
	r := getJSON(t, s, "/integrations/status")
	requireStatus(t, r, http.StatusOK)

	var out struct {
		Configured   bool `json:"configured"`
		Integrations []struct {
			Provider  string `json:"provider"`
			Level     string `json:"level"`
			Connected bool   `json:"connected"`
		} `json:"integrations"`
	}
	r.decode(t, &out)
	if !out.Configured {
		t.Fatalf("configured = false; the e2e server has a database, want true")
	}
	for _, in := range out.Integrations {
		if in.Connected {
			t.Errorf("provider %q/%q reports connected on a fresh org", in.Provider, in.Level)
		}
	}
}

// linearSettings is the create/update request body.
type linearSettings struct {
	CreationMode   string   `json:"creationMode"`
	TriggerStatus  string   `json:"triggerStatus,omitempty"`
	NameTemplate   string   `json:"nameTemplate,omitempty"`
	ConditionExpr  string   `json:"conditionExpr,omitempty"`
	ArchiveMode    string   `json:"archiveMode,omitempty"`
	AutoAddMembers []string `json:"autoAddMembers"`
	TeamID         string   `json:"teamId"`
}

// linearSettingsResp is the read shape returned by every settings mutation.
type linearSettingsResp struct {
	Connected bool `json:"connected"`
	Configs   []struct {
		SettingID    string `json:"settingId"`
		CreationMode string `json:"creationMode"`
		NameTemplate string `json:"nameTemplate"`
		TeamID       string `json:"teamId"`
	} `json:"configs"`
}

// TestLinearSettings_Unauthenticated asserts the config read needs a session.
func TestLinearSettings_Unauthenticated(t *testing.T) {
	r := getJSON(t, nil, "/integrations/linear/settings")
	requireStatus(t, r, http.StatusUnauthorized)
}

// TestLinearSettings_Lifecycle drives a config through create -> read -> update
// -> delete against the real Postgres, asserting persistence at each step and
// per-org isolation.
func TestLinearSettings_Lifecycle(t *testing.T) {
	s := newSession(t, "user_ls", "ls@e2e.test", "org_ls_lifecycle", "admin")

	// Empty to start.
	r := getJSON(t, s, "/integrations/linear/settings")
	requireStatus(t, r, http.StatusOK)
	var got linearSettingsResp
	r.decode(t, &got)
	if len(got.Configs) != 0 {
		t.Fatalf("fresh org has %d configs, want 0", len(got.Configs))
	}

	// Create.
	create := linearSettings{
		CreationMode:   "manual",
		NameTemplate:   "tkt-${{ linear.data.identifier }}",
		ArchiveMode:    "manual",
		AutoAddMembers: []string{},
		TeamID:         "team-e2e-alpha",
	}
	r = postJSON(t, s, "/integrations/linear/settings", create)
	requireStatus(t, r, http.StatusOK)
	r.decode(t, &got)
	if len(got.Configs) != 1 {
		t.Fatalf("after create: %d configs, want 1", len(got.Configs))
	}
	id := got.Configs[0].SettingID
	if id == "" {
		t.Fatal("created config has no settingId")
	}
	if got.Configs[0].TeamID != "team-e2e-alpha" {
		t.Errorf("teamId = %q, want team-e2e-alpha", got.Configs[0].TeamID)
	}

	// Read back independently.
	r = getJSON(t, s, "/integrations/linear/settings")
	requireStatus(t, r, http.StatusOK)
	r.decode(t, &got)
	if len(got.Configs) != 1 || got.Configs[0].SettingID != id {
		t.Fatalf("read-back mismatch: %+v", got.Configs)
	}

	// A different org must not see this config (tenant isolation).
	other := newSession(t, "user_other", "other@e2e.test", "org_ls_other", "admin")
	r = getJSON(t, other, "/integrations/linear/settings")
	requireStatus(t, r, http.StatusOK)
	var otherGot linearSettingsResp
	r.decode(t, &otherGot)
	if len(otherGot.Configs) != 0 {
		t.Fatalf("cross-tenant leak: other org sees %d configs", len(otherGot.Configs))
	}

	// Update.
	update := create
	update.NameTemplate = "chan-${{ linear.data.identifier }}"
	r = putJSON(t, s, "/integrations/linear/settings/"+id, update)
	requireStatus(t, r, http.StatusOK)
	r.decode(t, &got)
	if len(got.Configs) != 1 || got.Configs[0].NameTemplate != "chan-${{ linear.data.identifier }}" {
		t.Fatalf("after update: %+v", got.Configs)
	}

	// Delete.
	r = del(t, s, "/integrations/linear/settings/"+id)
	requireStatus(t, r, http.StatusOK)
	r.decode(t, &got)
	if len(got.Configs) != 0 {
		t.Fatalf("after delete: %d configs, want 0", len(got.Configs))
	}
}

// TestTemplateTest_RendersName drives the settings test endpoint against a
// built-in sample event end-to-end: the GitHub-Actions-expression template
// engine must render the sample's Linear identifier into the channel name.
func TestTemplateTest_RendersName(t *testing.T) {
	s := newSession(t, "user_tt", "tt@e2e.test", "org_tt", "admin")
	body := map[string]any{
		"nameTemplate": "tkt-${{ linear.data.identifier }}",
		"creationMode": "manual",
		"sampleId":     "issue.status_changed",
	}
	r := postJSON(t, s, "/integrations/linear/settings/test", body)
	requireStatus(t, r, http.StatusOK)

	var out struct {
		Name         string `json:"name"`
		Error        string `json:"error"`
		WouldCreate  bool   `json:"wouldCreate"`
		WouldArchive bool   `json:"wouldArchive"`
	}
	r.decode(t, &out)
	if out.Error != "" {
		t.Fatalf("unexpected template error: %s", out.Error)
	}
	if out.Name != "tkt-SKO-177" {
		t.Fatalf("rendered name = %q, want tkt-SKO-177", out.Name)
	}
}

// TestTemplateTest_BadEvent asserts an unparseable raw event is a clean 400, not
// a 500.
func TestTemplateTest_BadEvent(t *testing.T) {
	s := newSession(t, "user_tt2", "tt2@e2e.test", "org_tt2", "admin")
	body := map[string]any{
		"nameTemplate": "x",
		"creationMode": "manual",
		"event":        "{not valid json",
	}
	r := postJSON(t, s, "/integrations/linear/settings/test", body)
	requireStatus(t, r, http.StatusBadRequest)
}

// TestTemplateTest_Unauthenticated asserts the test endpoint needs a session.
func TestTemplateTest_Unauthenticated(t *testing.T) {
	body := map[string]any{"nameTemplate": "x", "creationMode": "manual", "sampleId": "issue.status_changed"}
	r := postJSON(t, nil, "/integrations/linear/settings/test", body)
	requireStatus(t, r, http.StatusUnauthorized)
}
