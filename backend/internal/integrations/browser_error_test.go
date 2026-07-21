package integrations

import (
	"net/url"
	"strings"
	"testing"

	"xolo/backend/internal/config"
)

func TestRedirectErrorURL(t *testing.T) {
	s := &Service{cfg: config.Config{App: config.AppConfig{PostLoginURL: "http://localhost:5173"}}}
	got := s.redirectErrorURL("slack", 401, ErrNoOrg)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Path != "/interrupted" {
		t.Fatalf("path = %q, want /interrupted", u.Path)
	}
	q := u.Query()
	if q.Get("status") != "401" || q.Get("code") != ErrNoOrg || q.Get("provider") != "slack" {
		t.Fatalf("query = %v, want status=401 code=%s provider=slack", q, ErrNoOrg)
	}
	// No raw exception text — only stable params.
	if strings.Contains(got, " ") || strings.Contains(strings.ToLower(got), "error=") {
		t.Fatalf("URL looks unsafe or free-form: %q", got)
	}
}
