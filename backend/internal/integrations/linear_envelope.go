package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// normalizeLinearType maps Linear's PascalCase webhook type to the NotifBuddy
// lowercase form and the entity key under which the former `data` object lives.
func normalizeLinearType(t string) (normalized, entityKey string) {
	switch strings.ToLower(strings.ReplaceAll(t, "_", "")) {
	case "issue":
		return "issue", "issue"
	case "comment":
		return "comment", "comment"
	case "workflowstate":
		return "workflow_state", "workflow_state"
	default:
		n := strings.ToLower(t)
		return n, n
	}
}

// normalizeLinearEnvelope transforms a raw Linear webhook body into the
// NotifBuddy envelope shape: lowercase type, typed entity key (no `data`),
// and — for comments — an injected `issue` object fetched via GraphQL.
//
//	{ type: "comment", action, actor, comment: {…}, issue: {…} }
func (s *Service) normalizeLinearEnvelope(ctx context.Context, orgID string, raw []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("normalize linear envelope: %w", err)
	}

	typ, _ := m["type"].(string)
	normType, entityKey := normalizeLinearType(typ)
	m["type"] = normType

	if data, ok := m["data"]; ok {
		delete(m, "data")
		m[entityKey] = data
	}

	if normType == "comment" {
		if err := s.injectCommentIssue(ctx, orgID, m); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// injectCommentIssue fetches the parent issue and sets linear.issue on the
// normalized envelope. Missing issue id is a no-op (nothing to inject); a
// GraphQL failure is returned so the writer can nack and retry.
func (s *Service) injectCommentIssue(ctx context.Context, orgID string, m map[string]any) error {
	comment, _ := m["comment"].(map[string]any)
	issueID := commentIssueID(comment)
	if issueID == "" {
		return nil
	}
	if orgID == "" {
		slog.WarnContext(ctx, "integrations: comment envelope: skip issue inject (no org)", "issue_id", issueID)
		return nil
	}
	issue, err := s.LinearIssueMapByID(ctx, orgID, issueID)
	if err != nil {
		return fmt.Errorf("inject comment issue %s: %w", issueID, err)
	}
	m["issue"] = issue
	return nil
}

func commentIssueID(comment map[string]any) string {
	if comment == nil {
		return ""
	}
	if id, ok := comment["issueId"].(string); ok && id != "" {
		return id
	}
	if nested, ok := comment["issue"].(map[string]any); ok {
		if id, ok := nested["id"].(string); ok {
			return id
		}
	}
	return ""
}
