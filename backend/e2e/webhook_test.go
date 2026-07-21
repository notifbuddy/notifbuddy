//go:build e2e

package e2e

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// linearSig computes the hex HMAC-SHA256 the Linear webhook handler expects.
func linearSig(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// TestLinearWebhook_BadSignature asserts a delivery with a wrong signature is
// rejected 401 (the webhook secret IS configured in e2e).
func TestLinearWebhook_BadSignature(t *testing.T) {
	body := []byte(`{"type":"Issue","action":"update","organizationId":"ws_e2e","webhookId":"wh_1","webhookTimestamp":1700000000000}`)
	r := do(t, nil, http.MethodPost, "/integrations/linear/webhook", body, map[string]string{
		"Linear-Signature": "deadbeef", // not the real HMAC
	})
	requireStatus(t, r, http.StatusUnauthorized)
}

// TestLinearWebhook_MissingSignature asserts a delivery with no signature header
// is rejected 401.
func TestLinearWebhook_MissingSignature(t *testing.T) {
	body := []byte(`{"type":"Issue","action":"update","organizationId":"ws_e2e","webhookId":"wh_2","webhookTimestamp":1700000000001}`)
	r := do(t, nil, http.MethodPost, "/integrations/linear/webhook", body, nil)
	requireStatus(t, r, http.StatusUnauthorized)
}

// TestLinearWebhook_ValidSignature asserts a correctly-signed delivery is
// accepted (202) and durably enqueued — no session required, this is an inbound
// provider callback.
func TestLinearWebhook_ValidSignature(t *testing.T) {
	body := []byte(`{"type":"Issue","action":"update","organizationId":"ws_e2e","webhookId":"wh_3","webhookTimestamp":1700000000002}`)
	r := do(t, nil, http.MethodPost, "/integrations/linear/webhook", body, map[string]string{
		"Linear-Signature": linearSig(linearSecret, body),
	})
	requireStatus(t, r, http.StatusAccepted)
}

// TestLinearWebhook_MissingType asserts a signed but typeless payload is a clean
// 400 (validated after the signature passes).
func TestLinearWebhook_MissingType(t *testing.T) {
	body := []byte(`{"action":"update","organizationId":"ws_e2e","webhookId":"wh_4","webhookTimestamp":1700000000003}`)
	r := do(t, nil, http.MethodPost, "/integrations/linear/webhook", body, map[string]string{
		"Linear-Signature": linearSig(linearSecret, body),
	})
	requireStatus(t, r, http.StatusBadRequest)
}

// TestSlackConnect_StartsOAuth asserts the Slack OAuth connect endpoint (for an
// authenticated, org-scoped caller) redirects to Slack's authorize page carrying
// a sealed state parameter — the start of the CSRF-protected OAuth handshake.
func TestSlackConnect_StartsOAuth(t *testing.T) {
	s := newSession(t, "user_conn", "conn@e2e.test", "org_conn", "admin")
	r := getJSON(t, s, "/integrations/slack/connect")
	if r.status != http.StatusFound && r.status != http.StatusTemporaryRedirect {
		t.Fatalf("connect status = %d, want a 3xx redirect\nbody: %s", r.status, r.body)
	}
	loc := r.header.Get("Location")
	if !strings.Contains(loc, "slack.com") {
		t.Fatalf("Location = %q, want a slack.com authorize URL", loc)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location %q: %v", loc, err)
	}
	if u.Query().Get("state") == "" {
		t.Fatalf("authorize URL carries no state param: %q", loc)
	}
}

// TestSlackConnect_OrglessSession asserts a signed-in-but-org-less caller cannot
// start a connect (there is no org to attach the integration to). Errors bounce
// to the SPA integrations page (NOT-32) instead of a plain-text API response.
func TestSlackConnect_OrglessSession(t *testing.T) {
	s := newSession(t, "user_noorg", "noorg@e2e.test", "", "")
	r := getJSON(t, s, "/integrations/slack/connect")
	loc := r.header.Get("Location")
	if strings.Contains(loc, "slack.com") {
		t.Fatalf("org-less caller started the Slack OAuth flow: %q", loc)
	}
	requireStatus(t, r, http.StatusFound)
	if !strings.Contains(loc, "/settings/integrations") || !strings.Contains(loc, "status=error") {
		t.Fatalf("Location = %q, want dashboard integrations redirect with status=error", loc)
	}
}

// TestSlackConnect_Unauthenticated asserts an anonymous connect does not start
// an OAuth flow (no redirect to Slack); errors bounce to the SPA (NOT-32).
func TestSlackConnect_Unauthenticated(t *testing.T) {
	r := getJSON(t, nil, "/integrations/slack/connect")
	loc := r.header.Get("Location")
	if strings.Contains(loc, "slack.com") {
		t.Fatalf("anonymous caller was redirected into the Slack OAuth flow: %q", loc)
	}
	requireStatus(t, r, http.StatusFound)
	if !strings.Contains(loc, "/settings/integrations") || !strings.Contains(loc, "status=error") {
		t.Fatalf("Location = %q, want dashboard integrations redirect with status=error", loc)
	}
}
