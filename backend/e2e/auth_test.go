//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"xolo/backend/e2e/fakeapis/session"
)

// TestPing_Unauthenticated asserts /ping rejects anonymous callers.
func TestPing_Unauthenticated(t *testing.T) {
	r := getJSON(t, nil, "/ping")
	requireStatus(t, r, http.StatusUnauthorized)
}

// TestPing_Authenticated asserts a forged session is accepted and /ping pongs.
func TestPing_Authenticated(t *testing.T) {
	s := newSession(t, "user_ping", "ping@e2e.test", "org_ping", "admin")
	r := getJSON(t, s, "/ping")
	requireStatus(t, r, http.StatusOK)

	var out struct {
		Message string `json:"message"`
	}
	r.decode(t, &out)
	if out.Message != "pong" {
		t.Fatalf("message = %q, want pong", out.Message)
	}
}

// TestMe_Unauthenticated asserts /me is 401 without a session.
func TestMe_Unauthenticated(t *testing.T) {
	r := getJSON(t, nil, "/me")
	requireStatus(t, r, http.StatusUnauthorized)
}

// TestMe_ReturnsIdentity asserts /me echoes the session's identity + active
// org — the whole round-trip through the authd fake.
func TestMe_ReturnsIdentity(t *testing.T) {
	s := newSession(t, "user_me_42", "me42@e2e.test", "org_me_42", "member")
	r := getJSON(t, s, "/me")
	requireStatus(t, r, http.StatusOK)

	var out struct {
		ID             string `json:"id"`
		Email          string `json:"email"`
		OrganizationID string `json:"organizationId"`
		Role           string `json:"role"`
	}
	r.decode(t, &out)
	if out.ID != s.userID {
		t.Errorf("id = %q, want %q", out.ID, s.userID)
	}
	if out.Email != s.email {
		t.Errorf("email = %q, want %q", out.Email, s.email)
	}
	if out.OrganizationID != s.orgID {
		t.Errorf("organizationId = %q, want %q", out.OrganizationID, s.orgID)
	}
	if out.Role != s.role {
		t.Errorf("role = %q, want %q", out.Role, s.role)
	}
}

// TestMe_TamperedCookie asserts a token signed with the wrong secret is
// rejected (proves the signature is actually validated, not just parsed).
func TestMe_TamperedCookie(t *testing.T) {
	bad, err := session.Mint("this-is-a-different-session-secret-000", session.Identity{
		UserID: "u", Email: "e@e.test", OrgID: "o", Role: "admin",
	})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	s := &forgedSession{cookie: bad}
	r := getJSON(t, s, "/me")
	requireStatus(t, r, http.StatusUnauthorized)
}

// TestUnknownRoute asserts an unmapped path 404s rather than 500s.
func TestUnknownRoute(t *testing.T) {
	r := getJSON(t, nil, "/no/such/endpoint")
	if r.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404\nbody: %s", r.status, r.body)
	}
}

// TestCORS_Preflight asserts the API answers a credentialed CORS preflight from
// the configured SPA origin with the matching allow-origin (never "*").
func TestCORS_Preflight(t *testing.T) {
	const origin = "http://localhost:5173"
	r := do(t, nil, http.MethodOptions, "/me", nil, map[string]string{
		"Origin":                         origin,
		"Access-Control-Request-Method":  "GET",
		"Access-Control-Request-Headers": "content-type",
	})
	if r.status != http.StatusOK && r.status != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 200/204\nbody: %s", r.status, r.body)
	}
	if got := r.header.Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
	if got := r.header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want true", got)
	}
}
