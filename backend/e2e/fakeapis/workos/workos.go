// Package workos fakes the subset of the WorkOS API the backend touches in the
// e2e flows.
package workos

import (
	"log"
	"net/http"

	"xolo/backend/e2e/fakeapis/respond"
)

// Host is the API hostname this fake answers for.
const Host = "api.workos.com"

// Handler returns the WorkOS fake. Extend the mux as more flows are covered.
func Handler() http.Handler {
	mux := http.NewServeMux()

	// GET /user_management/organization_memberships — /me lists the caller's
	// orgs. Empty is a valid answer (a fresh user in no WorkOS org); it also
	// keeps the response instant instead of the SDK retrying a dead host.
	mux.HandleFunc("GET /user_management/organization_memberships", func(w http.ResponseWriter, r *http.Request) {
		respond.JSON(w, http.StatusOK, map[string]any{
			"data":          []any{},
			"list_metadata": map[string]any{"before": nil, "after": nil},
		})
	})

	// Anything else WorkOS-shaped that a flow hits is a gap in the fake — make
	// it visible rather than silently wrong.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("fakeapis: unhandled workos path %s %s", r.Method, r.URL.Path)
		respond.JSON(w, http.StatusNotImplemented, map[string]string{"error": "fakeapis: workos path not faked: " + r.URL.Path})
	})
	return mux
}
