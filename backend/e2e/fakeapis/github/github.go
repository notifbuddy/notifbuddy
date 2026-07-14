// Package github fakes the GitHub API. Scaffold only — wired to a loud 501
// until a flow needs it; replace Handler's body with a real mux then.
package github

import (
	"net/http"

	"xolo/backend/e2e/fakeapis/respond"
)

// Host is the API hostname this fake answers for.
const Host = "api.github.com"

// Handler returns the GitHub fake.
func Handler() http.Handler {
	return respond.NotImplemented("github")
}
