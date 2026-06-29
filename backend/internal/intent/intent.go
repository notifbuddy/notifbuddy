// Package intent classifies the free-text body of an inbound comment (e.g. a
// GitHub PR comment or Linear issue comment that mentions @notifbuddy) into a
// single NotifBuddy command.
//
// This package is intentionally narrow: it is a natural-language → command
// classifier and nothing more. It does not read webhooks, talk to Slack, name
// or create channels, or store anything — those live in the service that calls
// Classify with the comment text and acts on the returned Intent.
//
// The classifier is LLM-backed and provider-agnostic: callers depend only on
// the Classifier interface. The Cloudflare Workers AI implementation lives in
// cloudflare.go. There is deliberately no keyword fallback; if the backend is
// unconfigured or the call fails, classification resolves to NoAction.
package intent

import "context"

// Intent is the command a comment resolves to. The zero value is NoAction so a
// missing/failed classification is safe by default.
type Intent string

const (
	// NoAction means the text is not a NotifBuddy command (the common case),
	// or the classifier could not produce a confident answer. It is also the
	// value returned on any error: unconfigured backend, model failure,
	// timeout, or unparseable model output.
	NoAction Intent = "no-action"
	// CreateChannel asks NotifBuddy to create the Slack channel linked to the
	// PR/ticket the comment is on (e.g. "@notifbuddy create slack channel",
	// "slack this plz").
	CreateChannel Intent = "create-channel"
	// CloseChannel asks NotifBuddy to close/archive that linked channel (e.g.
	// "@notifbuddy close the channel", "unsync this").
	CloseChannel Intent = "close-channel"
)

// Classifier turns free-text into a single Intent. Implementations must be safe
// for concurrent use.
//
// Classify never returns an error: every failure mode (unconfigured backend,
// model error, timeout, output it cannot parse) collapses to NoAction so a
// caller can act on the result unconditionally. Implementations log the
// underlying cause.
type Classifier interface {
	Classify(ctx context.Context, text string) Intent
}

// known reports whether s is one of the three command strings. The Cloudflare
// classifier uses it to reject any model output that is not exactly a command.
func known(s string) bool {
	switch Intent(s) {
	case CreateChannel, CloseChannel, NoAction:
		return true
	default:
		return false
	}
}
