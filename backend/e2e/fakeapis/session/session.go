// Package session forges the e2e stack's authenticated identity.
//
// The backend no longer validates cookies itself — it forwards the request's
// Cookie header to authd (Better Auth) and trusts the answer. In e2e, "authd"
// is the fake served by fakeapis, and the session token is our own format: a
// base64url JSON payload carrying the identity, signed with an HMAC keyed by
// the shared e2e secret. The fake verifies the HMAC, so a token minted with
// the wrong secret is rejected — tests can still prove sessions are validated,
// not just parsed.
//
// Two pieces of the stack must agree on one shared signed-in user + org:
//
//   - the session cookie the browser carries (written to the shared volume by
//     fakeapis, read by the Playwright UI suite), and
//   - the authd fake, which resolves that cookie back to this same identity so
//     /me returns a complete, org-scoped view.
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// CookieName is the session cookie the fake authd reads. Mirrors Better Auth's
// default cookie name so the e2e wiring stays recognizable.
const CookieName = "better-auth.session_token"

// The one identity the whole UI suite authenticates as. The authd fake and the
// minted cookie both describe this user/org, so /me renders the signed-in
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

// Identity is the claim set a token carries — everything the fake authd needs
// to answer get-session, get-active-member, and organization/list.
type Identity struct {
	UserID  string `json:"userId"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	OrgID   string `json:"orgId"`
	OrgName string `json:"orgName"`
	Role    string `json:"role"`
}

// Mint builds a signed session token for an arbitrary identity. OrgID/Role may
// be empty to model a signed-in-but-org-less user.
func Mint(secret string, id Identity) (string, error) {
	raw, err := json.Marshal(id)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + sign(secret, payload), nil
}

// Verify checks a token's HMAC and returns the identity it carries.
func Verify(secret, token string) (Identity, bool) {
	payload, sig, ok := strings.Cut(token, ".")
	if !ok || !hmac.Equal([]byte(sign(secret, payload)), []byte(sig)) {
		return Identity{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Identity{}, false
	}
	var id Identity
	if err := json.Unmarshal(raw, &id); err != nil || id.UserID == "" {
		return Identity{}, false
	}
	return id, true
}

func sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// Artifact is the JSON the UI test container reads to authenticate its browser
// (the session cookie plus the identity so specs can assert what /me returns).
type Artifact struct {
	Cookie  string `json:"cookie"`
	UserID  string `json:"userId"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	OrgID   string `json:"orgId"`
	OrgName string `json:"orgName"`
	Role    string `json:"role"`
}

// Write mints the shared identity's token and writes the artifact JSON to path
// (e.g. onto the shared certs volume) for the UI suite to pick up.
func Write(path, secret string) error {
	cookie, err := Mint(secret, Identity{
		UserID:  UserID,
		Email:   Email,
		Name:    FirstName + " " + LastName,
		OrgID:   OrgID,
		OrgName: OrgName,
		Role:    Role,
	})
	if err != nil {
		return fmt.Errorf("mint session: %w", err)
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
