// Package linear fakes the Linear API. Scaffold only — wired to a loud 501
// until a flow needs it; replace Handler's body with a real mux then.
package linear

import (
	"net/http"

	"xolo/backend/e2e/fakeapis/respond"
)

// Host is the API hostname this fake answers for.
const Host = "api.linear.app"

// Handler returns the Linear fake.
func Handler() http.Handler {
	return respond.NotImplemented("linear")
}
