// Package authd fakes the subset of authd (Better Auth) the backend touches:
// session resolution and the org views behind /me. Unlike the third-party
// fakes it is NOT reached through the MITM proxy — authd is first-party, the
// backend calls it directly over plain HTTP (auth.base_url points at the
// fakeapis container).
//
// Sessions are stateless: the whole identity lives in the HMAC-signed cookie
// minted by the session package, so tests can forge arbitrary users offline
// with the shared secret and the fake answers for them without registration.
package authd

import (
	"log"
	"net/http"

	"xolo/backend/e2e/fakeapis/respond"
	"xolo/backend/e2e/fakeapis/session"
)

// Handler returns the authd fake. secret verifies session-token HMACs.
func Handler(secret string) http.Handler {
	mux := http.NewServeMux()

	// identity resolves the request's session cookie, or nil.
	identity := func(r *http.Request) *session.Identity {
		c, err := r.Cookie(session.CookieName)
		if err != nil {
			return nil
		}
		id, ok := session.Verify(secret, c.Value)
		if !ok {
			return nil
		}
		return &id
	}

	// GET /api/auth/get-session — the session middleware's one required call.
	// Better Auth answers an anonymous request with a 200 "null" body, which the
	// backend maps to an unauthenticated request.
	mux.HandleFunc("GET /api/auth/get-session", func(w http.ResponseWriter, r *http.Request) {
		id := identity(r)
		if id == nil {
			respond.JSON(w, http.StatusOK, nil)
			return
		}
		respond.JSON(w, http.StatusOK, map[string]any{
			"session": map[string]any{"activeOrganizationId": id.OrgID},
			"user": map[string]any{
				"id":    id.UserID,
				"email": id.Email,
				"name":  id.Name,
				"image": "",
			},
		})
	})

	// GET /api/auth/organization/get-active-member — role lookup for the active
	// org.
	mux.HandleFunc("GET /api/auth/organization/get-active-member", func(w http.ResponseWriter, r *http.Request) {
		id := identity(r)
		if id == nil || id.OrgID == "" {
			respond.JSON(w, http.StatusBadRequest, map[string]string{"message": "no active organization"})
			return
		}
		respond.JSON(w, http.StatusOK, map[string]any{"role": id.Role})
	})

	// GET /api/auth/organization/list — the orgs behind /me's switcher. The
	// token is the source of truth, so the list is just its (single) org.
	mux.HandleFunc("GET /api/auth/organization/list", func(w http.ResponseWriter, r *http.Request) {
		id := identity(r)
		if id == nil {
			respond.JSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthenticated"})
			return
		}
		orgs := []any{}
		if id.OrgID != "" {
			name := id.OrgName
			if name == "" {
				name = id.OrgID
			}
			orgs = append(orgs, map[string]any{"id": id.OrgID, "name": name})
		}
		respond.JSON(w, http.StatusOK, orgs)
	})

	// GET /api/auth/organization/get-full-organization — the org profile +
	// member list behind the settings pages. Synthesized from the token: the
	// caller is the org's one member.
	mux.HandleFunc("GET /api/auth/organization/get-full-organization", func(w http.ResponseWriter, r *http.Request) {
		id := identity(r)
		orgID := r.URL.Query().Get("organizationId")
		if id == nil || orgID == "" || orgID != id.OrgID {
			respond.JSON(w, http.StatusForbidden, map[string]string{"message": "not a member of this organization"})
			return
		}
		name := id.OrgName
		if name == "" {
			name = id.OrgID
		}
		respond.JSON(w, http.StatusOK, map[string]any{
			"id":   id.OrgID,
			"name": name,
			"members": []any{
				map[string]any{
					"id":     "member_" + id.UserID,
					"userId": id.UserID,
					"role":   id.Role,
					"user": map[string]any{
						"email": id.Email,
						"name":  id.Name,
						"image": "",
					},
				},
			},
		})
	})

	// Anything else authd-shaped that a flow hits is a gap in the fake — make it
	// visible rather than silently wrong.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("fakeapis: unhandled authd path %s %s", r.Method, r.URL.Path)
		respond.JSON(w, http.StatusNotImplemented, map[string]string{"message": "fakeapis: authd path not faked: " + r.URL.Path})
	})
	return mux
}
