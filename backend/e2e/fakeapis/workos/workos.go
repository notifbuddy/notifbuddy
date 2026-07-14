// Package workos fakes the subset of the WorkOS API the backend touches in the
// e2e flows.
package workos

import (
	"log"
	"net/http"

	"xolo/backend/e2e/fakeapis/respond"
	"xolo/backend/e2e/fakeapis/session"
)

// Host is the API hostname this fake answers for.
const Host = "api.workos.com"

// Handler returns the WorkOS fake. Extend the mux as more flows are covered.
func Handler() http.Handler {
	mux := http.NewServeMux()

	// GET /user_management/organization_memberships — /me lists the caller's
	// orgs. Return the one shared e2e org (see the session package) so /me
	// resolves an active organization and the SPA renders the signed-in shell
	// with the org switcher populated. list_metadata.after is nil so the SDK's
	// iterator stops after this single page.
	mux.HandleFunc("GET /user_management/organization_memberships", func(w http.ResponseWriter, r *http.Request) {
		respond.JSON(w, http.StatusOK, map[string]any{
			"data": []any{
				map[string]any{
					"object":            "organization_membership",
					"id":                "om_e2e",
					"user_id":           session.UserID,
					"organization_id":   session.OrgID,
					"organization_name": session.OrgName,
					"status":            "active",
					"role":              map[string]any{"slug": session.Role},
				},
			},
			"list_metadata": map[string]any{"before": nil, "after": nil},
		})
	})

	// GET /organizations/{id} — /me's active-org profile lookup and the org
	// settings page. Echo the shared e2e org so the profile resolves.
	mux.HandleFunc("GET /organizations/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond.JSON(w, http.StatusOK, map[string]any{
			"object": "organization",
			"id":     r.PathValue("id"),
			"name":   session.OrgName,
		})
	})

	// GET /user_management/users/{id} — member-list hydration joins each
	// membership to the underlying user identity.
	mux.HandleFunc("GET /user_management/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond.JSON(w, http.StatusOK, map[string]any{
			"object":     "user",
			"id":         r.PathValue("id"),
			"email":      session.Email,
			"first_name": session.FirstName,
			"last_name":  session.LastName,
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
