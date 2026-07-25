package integrations

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNormalizeLinearType(t *testing.T) {
	cases := []struct {
		in, typ, key string
	}{
		{"Issue", "issue", "issue"},
		{"issue", "issue", "issue"},
		{"Comment", "comment", "comment"},
		{"WorkflowState", "workflow_state", "workflow_state"},
		{"workflow_state", "workflow_state", "workflow_state"},
		{"Reaction", "reaction", "reaction"},
		{"reaction", "reaction", "reaction"},
	}
	for _, c := range cases {
		typ, key := normalizeLinearType(c.in)
		if typ != c.typ || key != c.key {
			t.Errorf("normalizeLinearType(%q) = (%q, %q), want (%q, %q)", c.in, typ, key, c.typ, c.key)
		}
	}
}

func TestNormalizeLinearEnvelope_Issue(t *testing.T) {
	raw := []byte(`{
		"action": "update",
		"type": "Issue",
		"actor": {"name": "Ada"},
		"data": {"id": "iss1", "identifier": "NOT-1", "state": {"name": "Todo"}, "teamId": "t1"}
	}`)
	s := &Service{}
	got, err := s.normalizeLinearEnvelope(context.Background(), "", raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "issue" {
		t.Errorf("type = %v, want issue", got["type"])
	}
	if _, ok := got["data"]; ok {
		t.Error("data key must be removed")
	}
	issue, ok := got["issue"].(map[string]any)
	if !ok {
		t.Fatalf("issue missing: %#v", got)
	}
	if issue["identifier"] != "NOT-1" {
		t.Errorf("identifier = %v", issue["identifier"])
	}
	if got["action"] != "update" {
		t.Errorf("action = %v", got["action"])
	}
}

func TestNormalizeLinearEnvelope_Comment_NoOrgSkipsInject(t *testing.T) {
	raw := []byte(`{
		"action": "create",
		"type": "Comment",
		"data": {"id": "c1", "body": "hi", "issueId": "iss1"}
	}`)
	s := &Service{}
	got, err := s.normalizeLinearEnvelope(context.Background(), "", raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "comment" {
		t.Errorf("type = %v, want comment", got["type"])
	}
	if _, ok := got["comment"].(map[string]any); !ok {
		t.Fatalf("comment missing: %#v", got)
	}
	if _, ok := got["issue"]; ok {
		t.Error("issue must not be injected without org")
	}
}

func TestCommentIssueID(t *testing.T) {
	if id := commentIssueID(map[string]any{"issueId": "a"}); id != "a" {
		t.Errorf("issueId = %q", id)
	}
	if id := commentIssueID(map[string]any{"issue": map[string]any{"id": "b"}}); id != "b" {
		t.Errorf("nested = %q", id)
	}
	b, _ := json.Marshal(map[string]any{"x": 1})
	_ = b
}
