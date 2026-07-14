// Package session forges the e2e stack's authenticated identity.
//
// It centralizes one shared signed-in user + organization that two pieces of
// the stack must agree on:
//
//   - the sealed wos_session cookie the browser carries (written to the shared
//     volume by fakeapis, read by the Playwright UI suite), and
//   - the WorkOS organization-membership fake, which echoes this same org so
//     /me returns a complete, org-scoped identity.
//
// Sealing mirrors the Go e2e harness: an *unsigned* JWT payload sealed with the
// cookie password. AuthenticateSession only symmetric-unseals the cookie and
// base64-decodes the payload — it never verifies the JWT signature — so this is
// all a forged session needs. No live WorkOS is involved.
package session

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	workos "github.com/workos/workos-go/v9"
)

// The one identity the whole UI suite authenticates as. The membership fake and
// the sealed cookie both describe this user/org, so /me renders the signed-in
// dashboard (org switcher, admin-only controls, etc.).
const (
	UserID    = "user_e2e"
	Email     = "jane@e2e.test"
	FirstName = "Jane"
	LastName  = "Doe"
	OrgID     = "org_e2e"
	OrgName   = "NotifBuddy E2E"
	Role      = "admin"
)

// Seal builds the wos_session cookie value the backend accepts for the shared
// identity, using the same cookie password the server is configured with.
func Seal(cookiePassword string) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	claims := map[string]any{
		"sid":    "session_e2e_" + UserID,
		"org_id": OrgID,
		"role":   Role,
		"exp":    time.Now().Add(time.Hour).Unix(),
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(cb)
	accessToken := header + "." + payload + ".e2e-unsigned"

	first, last := FirstName, LastName
	user := &workos.User{ID: UserID, Email: Email, FirstName: &first, LastName: &last}
	return workos.SealSessionFromAuthResponse(accessToken, "refresh_e2e", user, nil, cookiePassword)
}

// Artifact is the JSON the UI test container reads to authenticate its browser
// (the sealed cookie plus the identity so specs can assert what /me returns).
type Artifact struct {
	Cookie  string `json:"cookie"`
	UserID  string `json:"userId"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	OrgID   string `json:"orgId"`
	OrgName string `json:"orgName"`
	Role    string `json:"role"`
}

// Write seals a session and writes the artifact JSON to path (e.g. onto the
// shared certs volume) for the UI suite to pick up.
func Write(path, cookiePassword string) error {
	cookie, err := Seal(cookiePassword)
	if err != nil {
		return fmt.Errorf("seal session: %w", err)
	}
	a := Artifact{
		Cookie:  cookie,
		UserID:  UserID,
		Email:   Email,
		Name:    FirstName + " " + LastName,
		OrgID:   OrgID,
		OrgName: OrgName,
		Role:    Role,
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
