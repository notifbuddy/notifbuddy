//go:build e2e

// Package e2e is a black-box end-to-end suite for the Xolo backend. It talks to
// a fully wired server (real Postgres, real pub/sub, real HTTP stack) over the
// network exactly like the SPA would — no in-process handlers, no mocks of our
// own code. External SaaS is disabled or stubbed by config.e2e.yaml.
//
// The suite is gated behind the `e2e` build tag so `go test ./...` never picks
// it up; run it via backend/e2e/run.sh (docker compose) or point E2E_BASE_URL at
// a running server. With E2E_BASE_URL unset, TestMain skips the whole package.
//
// Authentication: the WorkOS session cookie is a symmetric-sealed blob wrapping
// an *unsigned* JWT whose signature AuthenticateSession never checks (see
// parseJWTPayload in the SDK). So the harness forges arbitrary sessions offline
// by sealing a hand-built JWT with the same cookie password the server uses —
// no live WorkOS required.
package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	workos "github.com/workos/workos-go/v9"
)

var (
	baseURL        string
	cookiePassword string
	linearSecret   string
)

func TestMain(m *testing.M) {
	baseURL = os.Getenv("E2E_BASE_URL")
	if baseURL == "" {
		fmt.Println("e2e: E2E_BASE_URL not set — skipping the e2e suite (run via backend/e2e/run.sh)")
		os.Exit(0)
	}
	cookiePassword = os.Getenv("WORKOS_COOKIE_PASSWORD")
	if cookiePassword == "" {
		fmt.Println("e2e: WORKOS_COOKIE_PASSWORD not set — cannot forge sessions")
		os.Exit(1)
	}
	linearSecret = os.Getenv("LINEAR_WEBHOOK_SECRET")

	if err := waitForServer(baseURL, 60*time.Second); err != nil {
		fmt.Printf("e2e: server never became ready: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// waitForServer polls /ping until the server answers HTTP (any status — an
// unauthenticated /ping is a 401, which still proves the stack is up).
func waitForServer(base string, timeout time.Duration) error {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/ping")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out after %s: %w", timeout, lastErr)
}

// session identifies a forged caller.
type session struct {
	userID string
	email  string
	orgID  string
	role   string
	cookie string // sealed wos_session value
}

// newSession forges a sealed session cookie for the given identity. orgID/role
// may be empty to model a signed-in-but-org-less user.
func newSession(t *testing.T, userID, email, orgID, role string) *session {
	t.Helper()
	cookie, err := sealSession(cookiePassword, userID, email, orgID, role)
	if err != nil {
		t.Fatalf("forge session: %v", err)
	}
	return &session{userID: userID, email: email, orgID: orgID, role: role, cookie: cookie}
}

// sealSession builds the wos_session cookie value the server accepts: a WorkOS
// SessionData sealed with the cookie password, carrying an unsigned JWT whose
// payload holds the org/role/exp claims the server reads.
func sealSession(password, userID, email, orgID, role string) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	claims := map[string]any{
		"sid":    "session_e2e_" + userID,
		"org_id": orgID,
		"role":   role,
		"exp":    time.Now().Add(time.Hour).Unix(),
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(cb)
	accessToken := header + "." + payload + ".e2e-unsigned"

	user := &workos.User{ID: userID, Email: email}
	return workos.SealSessionFromAuthResponse(accessToken, "refresh_e2e", user, nil, password)
}

// response is a decoded HTTP response.
type response struct {
	status int
	body   []byte
	header http.Header
}

// decode unmarshals the JSON body into v (fatal on error).
func (r *response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode body (%d): %v\nbody: %s", r.status, err, r.body)
	}
}

// noRedirectClient never follows redirects, so tests can inspect 302 Location +
// Set-Cookie headers (integration connect flow).
var noRedirectClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// do issues an HTTP request. Pass a *session for an authenticated call or nil to
// stay anonymous. body may be nil, a []byte, or any JSON-marshalable value.
func do(t *testing.T, sess *session, method, path string, body any, headers map[string]string) *response {
	t.Helper()

	var rdr io.Reader
	if body != nil {
		switch b := body.(type) {
		case []byte:
			rdr = bytes.NewReader(b)
		default:
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal request body: %v", err)
			}
			rdr = bytes.NewReader(raw)
		}
	}

	req, err := http.NewRequest(method, baseURL+path, rdr)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sess != nil {
		req.AddCookie(&http.Cookie{Name: "wos_session", Value: sess.cookie})
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return &response{status: resp.StatusCode, body: raw, header: resp.Header}
}

// getJSON / postJSON / putJSON / del are thin verb helpers.
func getJSON(t *testing.T, s *session, path string) *response {
	return do(t, s, http.MethodGet, path, nil, nil)
}
func postJSON(t *testing.T, s *session, path string, body any) *response {
	return do(t, s, http.MethodPost, path, body, nil)
}
func putJSON(t *testing.T, s *session, path string, body any) *response {
	return do(t, s, http.MethodPut, path, body, nil)
}
func del(t *testing.T, s *session, path string) *response {
	return do(t, s, http.MethodDelete, path, nil, nil)
}

// requireStatus fails the test unless the response status matches.
func requireStatus(t *testing.T, r *response, want int) {
	t.Helper()
	if r.status != want {
		t.Fatalf("status = %d, want %d\nbody: %s", r.status, want, r.body)
	}
}
