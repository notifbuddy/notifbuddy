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
	wantTitle, wantMsg := browserErrorCopy("slack", ErrNoOrg)
	if q.Get("title") != wantTitle || q.Get("message") != wantMsg {
		t.Fatalf("title/message = %q / %q, want %q / %q", q.Get("title"), q.Get("message"), wantTitle, wantMsg)
	}
	if strings.Contains(strings.ToLower(got), "error=") {
		t.Fatalf("URL looks free-form: %q", got)
	}
}

func TestBrowserErrorCopy_NoOrg(t *testing.T) {
	title, msg := browserErrorCopy("linear", ErrNoOrg)
	if !strings.Contains(title, "Linear") || !strings.Contains(msg, "organization") {
		t.Fatalf("title=%q msg=%q", title, msg)
	}
}
