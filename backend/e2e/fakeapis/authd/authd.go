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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"xolo/backend/e2e/fakeapis/respond"
	"xolo/backend/e2e/fakeapis/session"
)

type invitation struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Status         string `json:"status"`
	ExpiresAt      string `json:"expiresAt"`
	Role           string `json:"role"`
	OrganizationID string `json:"organizationId"`
}

// invitationStore holds the invitations an e2e run creates, keyed by org. Better
// Auth's own store is a database; this is the in-memory stand-in.
type invitationStore struct {
	mu   sync.Mutex
	seq  int
	byID map[string]*invitation
}

func newInvitationStore() *invitationStore {
	return &invitationStore{byID: map[string]*invitation{}}
}

func (s *invitationStore) create(orgID, email, role string) invitation {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, inv := range s.byID {
		if inv.OrganizationID == orgID && strings.EqualFold(inv.Email, email) && inv.Status == "pending" {
			inv.Status = "canceled"
		}
	}
	s.seq++
	inv := &invitation{
		ID:             fmt.Sprintf("invitation_%03d", s.seq),
		Email:          email,
		Status:         "pending",
		ExpiresAt:      time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339),
		Role:           role,
		OrganizationID: orgID,
	}
	s.byID[inv.ID] = inv
	return *inv
}

func (s *invitationStore) list(orgID string) []invitation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []invitation{}
	for i := 1; i <= s.seq; i++ {
		inv, ok := s.byID[fmt.Sprintf("invitation_%03d", i)]
		if ok && inv.OrganizationID == orgID {
			out = append(out, *inv)
		}
	}
	return out
}

// cancel marks an invitation canceled, the state a revoke lands it in.
func (s *invitationStore) cancel(orgID, id string) (invitation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.byID[id]
	if !ok || inv.OrganizationID != orgID {
		return invitation{}, false
	}
	inv.Status = "canceled"
	return *inv, true
}

// Handler returns the authd fake. secret verifies session-token HMACs.
func Handler(secret string) http.Handler {
	mux := http.NewServeMux()
	invites := newInvitationStore()

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

	// GET /api/auth/organization/list-invitations — every invitation the org has,
	// in whatever state, exactly as Better Auth returns them.
	mux.HandleFunc("GET /api/auth/organization/list-invitations", func(w http.ResponseWriter, r *http.Request) {
		id := identity(r)
		orgID := r.URL.Query().Get("organizationId")
		if id == nil || orgID == "" || orgID != id.OrgID {
			respond.JSON(w, http.StatusForbidden, map[string]string{"message": "not a member of this organization"})
			return
		}
		respond.JSON(w, http.StatusOK, invites.list(orgID))
	})

	// POST /api/auth/organization/invite-member — creates a pending invitation
	// and returns it unwrapped.
	mux.HandleFunc("POST /api/auth/organization/invite-member", func(w http.ResponseWriter, r *http.Request) {
		id := identity(r)
		var body struct {
			OrganizationID string `json:"organizationId"`
			Email          string `json:"email"`
			Role           string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respond.JSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})
			return
		}
		if id == nil || body.OrganizationID == "" || body.OrganizationID != id.OrgID {
			respond.JSON(w, http.StatusForbidden, map[string]string{"message": "not a member of this organization"})
			return
		}
		if body.Email == "" {
			respond.JSON(w, http.StatusBadRequest, map[string]string{"message": "email is required"})
			return
		}
		respond.JSON(w, http.StatusOK, invites.create(body.OrganizationID, body.Email, body.Role))
	})

	// POST /api/auth/organization/cancel-invitation — wraps the canceled
	// invitation under "invitation", the shape Better Auth answers with.
	mux.HandleFunc("POST /api/auth/organization/cancel-invitation", func(w http.ResponseWriter, r *http.Request) {
		id := identity(r)
		var body struct {
			InvitationID string `json:"invitationId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			respond.JSON(w, http.StatusBadRequest, map[string]string{"message": "invalid body"})
			return
		}
		if id == nil {
			respond.JSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthenticated"})
			return
		}
		inv, ok := invites.cancel(id.OrgID, body.InvitationID)
		if !ok {
			respond.JSON(w, http.StatusBadRequest, map[string]string{"message": "invitation not found"})
			return
		}
		respond.JSON(w, http.StatusOK, map[string]any{"invitation": inv})
	})

	// Anything else authd-shaped that a flow hits is a gap in the fake — make it
	// visible rather than silently wrong.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("fakeapis: unhandled authd path %s %s", r.Method, r.URL.Path)
		respond.JSON(w, http.StatusNotImplemented, map[string]string{"message": "fakeapis: authd path not faked: " + r.URL.Path})
	})
	return mux
}
