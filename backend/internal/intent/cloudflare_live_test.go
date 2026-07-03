package intent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"xolo/backend/internal/config"
)

// TestClassify_Live hits the real Cloudflare Workers AI API using the configured
// credentials. It is skipped unless RUN_LIVE_INTENT=1 so it never runs in the
// normal suite or CI (it needs network + a valid token + spends Neurons).
//
//	cd backend && RUN_LIVE_INTENT=1 go test ./internal/intent/ -run Live -v
//
// It loads backend/.env (for CF_API_TOKEN) and backend/config.local.yaml the same way
// the server does, then asserts a few representative phrasings classify as
// expected. Small models aren't perfectly deterministic, so this is a smoke
// test of the wiring (auth, endpoint, parsing), not a strict accuracy gate.
func TestClassify_Live(t *testing.T) {
	if os.Getenv("RUN_LIVE_INTENT") != "1" {
		t.Skip("set RUN_LIVE_INTENT=1 to run the live Cloudflare Workers AI test")
	}

	// Resolve backend/ (two dirs up from internal/intent) and load .env + config
	// from there regardless of the test's working directory.
	backendDir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve backend dir: %v", err)
	}
	if err := godotenv.Load(filepath.Join(backendDir, ".env")); err != nil {
		t.Logf("note: could not load .env (%v); relying on real environment", err)
	}
	t.Setenv("CONFIG_FILE", filepath.Join(backendDir, "config.yaml"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Cloudflare.AccountID == "" || cfg.Cloudflare.APIToken == "" {
		t.Fatalf("cloudflare not configured: account_id=%q api_token set=%t",
			cfg.Cloudflare.AccountID, cfg.Cloudflare.APIToken != "")
	}
	t.Logf("using model %q on account %s", cfg.Cloudflare.Model, cfg.Cloudflare.AccountID)

	c := NewCloudflareClassifier(cfg.Cloudflare)

	cases := []struct {
		text string
		want Intent
	}{
		{"@notifbuddy create slack channel", CreateChannel},
		{"@notifbuddy slack this plz", CreateChannel},
		{"@notifbuddy close the channel", CloseChannel},
		{"@notifbuddy archive this, we're done", CloseChannel},
		{"lgtm, merging now", NoAction},
		{"can someone review this when they get a chance?", NoAction},
	}
	for _, tc := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		got := c.Classify(ctx, tc.text)
		cancel()
		status := "OK"
		if got != tc.want {
			status = "MISMATCH"
		}
		t.Logf("[%s] %-55q -> %-15s (want %s)", status, tc.text, got, tc.want)
		if got != tc.want {
			t.Errorf("Classify(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}
