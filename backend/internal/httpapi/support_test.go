package httpapi

import (
	"context"
	"testing"

	"xolo/backend/internal/auth"
	"xolo/backend/internal/config"
)

const (
	testWebsiteToken = "wt_test"
	testHMACToken    = "hmac_tok"
	// HMAC-SHA256("user1") under testHMACToken.
	testIdentifierHash = "5c4d8f080f6fddccee1ba1ede658be3c97a70efe8177f2e67e88cb9d2984bca5"
)

func TestGetSupportChat_UnauthenticatedGetsWidgetConfigOnly(t *testing.T) {
	h := Handler{chatwoot: config.ChatwootConfig{WebsiteToken: testWebsiteToken, HMACToken: testHMACToken}}
	resp, err := h.GetSupportChat(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Enabled {
		t.Error("enabled = false, want true — signed-out visitors get the widget too")
	}
	if got := resp.WebsiteToken.Or(""); got != testWebsiteToken {
		t.Errorf("websiteToken = %q, want %q", got, testWebsiteToken)
	}
	if got := resp.BaseUrl.Or(""); got != defaultChatwootBaseURL {
		t.Errorf("baseUrl = %q, want %q", got, defaultChatwootBaseURL)
	}
	if resp.Identifier.IsSet() || resp.IdentifierHash.IsSet() || resp.Email.IsSet() {
		t.Error("identity fields set without a session")
	}
}

func TestGetSupportChat_DisabledWithoutWebsiteToken(t *testing.T) {
	h := Handler{chatwoot: config.ChatwootConfig{HMACToken: testHMACToken}}
	ctx := auth.ContextWithUser(context.Background(), &auth.SessionUser{ID: "user1", Email: "jane@example.com"})
	resp, err := h.GetSupportChat(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Enabled {
		t.Error("enabled = true, want false without a website token")
	}
	if resp.BaseUrl.IsSet() || resp.WebsiteToken.IsSet() || resp.Email.IsSet() {
		t.Error("disabled response leaked configuration or identity")
	}
}

func TestGetSupportChat_SelfHostedBaseURL(t *testing.T) {
	h := Handler{chatwoot: config.ChatwootConfig{
		BaseURL:      "https://support.notifbuddy.com/",
		WebsiteToken: testWebsiteToken,
	}}
	resp, err := h.GetSupportChat(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.BaseUrl.Or(""); got != "https://support.notifbuddy.com" {
		t.Errorf("baseUrl = %q, want the configured origin with no trailing slash", got)
	}
}

func TestGetSupportChat_IdentifiesUser(t *testing.T) {
	h := Handler{chatwoot: config.ChatwootConfig{WebsiteToken: testWebsiteToken, HMACToken: testHMACToken}}
	ctx := auth.ContextWithUser(context.Background(), &auth.SessionUser{
		ID:        "user1",
		Email:     "jane@example.com",
		FirstName: "Jane",
		LastName:  "Doe",
	})
	resp, err := h.GetSupportChat(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The identifier is the user id, not the email: hashing the email would
	// strand a user's history the moment they change their address.
	if got := resp.Identifier.Or(""); got != "user1" {
		t.Errorf("identifier = %q, want user1", got)
	}
	if got := resp.IdentifierHash.Or(""); got != testIdentifierHash {
		t.Errorf("identifierHash = %q, want %q", got, testIdentifierHash)
	}
	if got := resp.Email.Or(""); got != "jane@example.com" {
		t.Errorf("email = %q, want jane@example.com", got)
	}
	if got := resp.Name.Or(""); got != "Jane Doe" {
		t.Errorf("name = %q, want Jane Doe", got)
	}
	if resp.OrganizationId.IsSet() {
		t.Error("organizationId present for an org-less session")
	}
}

func TestGetSupportChat_IdentityUnhashedWithoutToken(t *testing.T) {
	h := Handler{chatwoot: config.ChatwootConfig{WebsiteToken: testWebsiteToken}}
	ctx := auth.ContextWithUser(context.Background(), &auth.SessionUser{ID: "user1", Email: "jane@example.com"})
	resp, err := h.GetSupportChat(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IdentifierHash.IsSet() {
		t.Error("identifierHash set without an identity-validation token")
	}
	if got := resp.Identifier.Or(""); got != "user1" {
		t.Errorf("identifier = %q, want user1 — identity is still sent, just unverified", got)
	}
}

func TestHMACHex_KnownVector(t *testing.T) {
	if got := hmacHex(testHMACToken, "user1"); got != testIdentifierHash {
		t.Errorf("hmacHex = %q, want %q", got, testIdentifierHash)
	}
}
