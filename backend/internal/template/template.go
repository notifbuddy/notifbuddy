// Package template implements GitHub Actions expression syntax for two jobs:
//
//   - Render — expand ${{ <expr> }} interpolations in a string (channel naming),
//     e.g. "tkt-${{ linear.data.identifier }}".
//   - Evaluate — evaluate a single expression for truthiness (channel-creation
//     conditional), e.g. "linear.action == 'update' && linear.data.state.name == 'Done'".
//
// It is provider-agnostic: expressions run against a forwarded event envelope
// ({event_type, linear|github: raw}), so the same engine serves both the Linear
// and (future) GitHub channel rules.
//
// The dialect is GitHub Actions expressions (https://docs.github.com/actions
// → "Evaluate expressions"): single-quoted strings with ” escaping, numbers,
// booleans, null; operators ! < <= > >= == != && ||; property/index access and
// the * filter; and the functions contains, startsWith, endsWith, format, join,
// toJSON, fromJSON. The CI-only functions (hashFiles, success, always,
// cancelled, failure) are intentionally unsupported and raise an error rather
// than silently returning false — there is no CI context to evaluate them.
package template

import (
	"encoding/json"
	"fmt"
)

// Event is the forwarded envelope expressions evaluate against. Exactly one of
// Linear/GitHub is populated, matching EventType. The raw provider payload is a
// decoded JSON object so expressions can walk it (linear.data.state.name, …).
type Event struct {
	EventType string         `json:"event_type"`
	Linear    map[string]any `json:"linear,omitempty"`
	GitHub    map[string]any `json:"github,omitempty"`
}

// context builds the root lookup map exposed to expressions: the top-level
// names an expression may reference (event_type, linear, github).
func (e Event) context() map[string]any {
	return map[string]any{
		"event_type": e.EventType,
		"linear":     e.Linear,
		"github":     e.GitHub,
	}
}

// Engine renders name templates and evaluates boolean conditionals.
// Implementations must be safe for concurrent use.
type Engine interface {
	Render(tmpl string, evt Event) (string, error)
	Evaluate(expr string, evt Event) (bool, error)
}

// engine is the default Engine. It is stateless.
type engine struct{}

// New returns the default GitHub-Actions-expression engine.
func New() Engine { return engine{} }

// Evaluate parses expr and returns its truthiness against evt. A parse or
// evaluation error (including use of an unsupported function) is returned.
func (engine) Evaluate(expr string, evt Event) (bool, error) {
	v, err := evalExpr(expr, evt.context())
	if err != nil {
		return false, err
	}
	return truthy(v), nil
}

// Render expands every ${{ ... }} occurrence in tmpl with the (stringified)
// value of the inner expression evaluated against evt. Text outside ${{ }} is
// literal. An error in any embedded expression aborts the whole render.
func (engine) Render(tmpl string, evt Event) (string, error) {
	return renderTemplate(tmpl, evt.context())
}

// ParseEvent decodes a raw envelope JSON into an Event. Used by callers that
// accept a pasted/sample event for the test feature.
func ParseEvent(raw []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(raw, &e); err != nil {
		return Event{}, fmt.Errorf("template: parse event: %w", err)
	}
	return e, nil
}
