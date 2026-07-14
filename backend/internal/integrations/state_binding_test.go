package integrations

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The OAuth callback must only accept a state whose nonce matches the cookie the
// connect step set on the initiating browser (and that hasn't expired) — this is
// what stops the account-linking CSRF where an attacker's sealed state is
// completed by a victim's browser.
func TestVerifyState(t *testing.T) {
	s := &Service{}
	const nonce = "nonce-abc-123"
	fresh := oauthState{OrgID: "org_1", Nonce: nonce, IssuedAt: time.Now().Unix()}

	req := func(name, val string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/integrations/slack/callback", nil)
		if name != "" {
			r.AddCookie(&http.Cookie{Name: name, Value: val})
		}
		return r
	}

	t.Run("matching cookie passes", func(t *testing.T) {
		if err := s.verifyState(req(stateCookieName("slack"), nonce), "slack", fresh); err != nil {
			t.Fatalf("want nil, got %v", err)
		}
	})
	t.Run("nonce mismatch rejected", func(t *testing.T) {
		if err := s.verifyState(req(stateCookieName("slack"), "other"), "slack", fresh); err == nil {
			t.Fatal("want error for mismatched nonce")
		}
	})
	t.Run("missing cookie rejected", func(t *testing.T) {
		if err := s.verifyState(req("", ""), "slack", fresh); err == nil {
			t.Fatal("want error when no cookie present")
		}
	})
	t.Run("wrong provider cookie rejected", func(t *testing.T) {
		// A linear cookie must not satisfy a slack callback.
		if err := s.verifyState(req(stateCookieName("linear"), nonce), "slack", fresh); err == nil {
			t.Fatal("want error when only the other provider's cookie is set")
		}
	})
	t.Run("expired state rejected", func(t *testing.T) {
		stale := oauthState{OrgID: "org_1", Nonce: nonce, IssuedAt: time.Now().Add(-oauthStateTTL - time.Minute).Unix()}
		if err := s.verifyState(req(stateCookieName("slack"), nonce), "slack", stale); err == nil {
			t.Fatal("want error for expired state")
		}
	})
}

// sealState/openState round-trips through the real encryptor and stamps IssuedAt.
func TestSealStateStampsIssuedAt(t *testing.T) {
	s := newTestService(t)
	sealed, err := s.sealState(oauthState{OrgID: "org_1", Nonce: "n"})
	if err != nil {
		t.Fatalf("sealState: %v", err)
	}
	got, err := s.openState(sealed)
	if err != nil {
		t.Fatalf("openState: %v", err)
	}
	if got.IssuedAt == 0 {
		t.Fatal("IssuedAt should be stamped by sealState")
	}
	if got.OrgID != "org_1" || got.Nonce != "n" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}
