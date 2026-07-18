package integrations

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xolo/backend/internal/config"
	"xolo/backend/internal/crypto"
)

func newProxyTestService(t *testing.T) *Service {
	t.Helper()
	enc, err := crypto.NewLocalKeyEncryptor(make([]byte, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	cfg := config.Config{}
	cfg.Server.PublicBaseURL = "https://api.example.com"
	return New(nil, enc, cfg, nil, nil)
}

func (s *Service) sealAssetPayload(t *testing.T, p assetProxyPayload) string {
	t.Helper()
	raw, _ := json.Marshal(p)
	sealed, err := s.enc.Encrypt(raw)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(sealed)
}

func TestAssetProxyToken_RoundTrip(t *testing.T) {
	s := newProxyTestService(t)
	u, err := s.LinearAssetProxyURL("org1", "https://uploads.linear.app/a/b/c")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	const prefix = "https://api.example.com/integrations/linear/asset/"
	if !strings.HasPrefix(u, prefix) {
		t.Fatalf("url = %q", u)
	}
	p, err := s.openAssetProxyToken(strings.TrimPrefix(u, prefix))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if p.OrgID != "org1" || p.FileURL != "https://uploads.linear.app/a/b/c" {
		t.Errorf("payload wrong: %+v", p)
	}
	// The minted expiry must be ~5 minutes out — just enough for Slack's
	// one-time fetch, so a leaked URL dies almost immediately.
	ttl := time.Until(time.Unix(p.Exp, 0))
	if ttl < 4*time.Minute || ttl > 6*time.Minute {
		t.Errorf("expiry not ~5m out: %v", ttl)
	}
}

func TestAssetProxyToken_ExpiredRejected(t *testing.T) {
	s := newProxyTestService(t)
	tok := s.sealAssetPayload(t, assetProxyPayload{
		OrgID: "org1", FileURL: "https://uploads.linear.app/a", Exp: time.Now().Add(-time.Minute).Unix(),
	})
	if _, err := s.openAssetProxyToken(tok); err == nil {
		t.Fatal("expired token must be rejected")
	}
}

func TestAssetProxyToken_MissingExpiryRejected(t *testing.T) {
	s := newProxyTestService(t)
	tok := s.sealAssetPayload(t, assetProxyPayload{OrgID: "org1", FileURL: "https://uploads.linear.app/a"})
	if _, err := s.openAssetProxyToken(tok); err == nil {
		t.Fatal("token without expiry must be rejected (fail closed)")
	}
}

func TestAssetProxyToken_DisallowedHostRejected(t *testing.T) {
	s := newProxyTestService(t)
	for _, target := range []string{
		"https://evil.example.com/x",
		"http://uploads.linear.app/x", // https only
		"https://uploads.linear.app.evil.com/x",
	} {
		tok := s.sealAssetPayload(t, assetProxyPayload{
			OrgID: "org1", FileURL: target, Exp: time.Now().Add(time.Hour).Unix(),
		})
		if _, err := s.openAssetProxyToken(tok); err == nil {
			t.Errorf("target %q must be rejected", target)
		}
	}
}

func TestAssetProxyToken_TamperRejected(t *testing.T) {
	s := newProxyTestService(t)
	u, err := s.LinearAssetProxyURL("org1", "https://uploads.linear.app/a")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	tok := u[strings.LastIndex(u, "/")+1:]
	raw, _ := base64.RawURLEncoding.DecodeString(tok)
	raw[len(raw)-1] ^= 0xFF // flip a ciphertext bit
	if _, err := s.openAssetProxyToken(base64.RawURLEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("tampered token must be rejected")
	}
}

func TestLinearAssetProxyURL_RequiresPublicBaseURL(t *testing.T) {
	enc, _ := crypto.NewLocalKeyEncryptor(make([]byte, 32))
	s := New(nil, enc, config.Config{}, nil, nil) // no public_base_url
	if _, err := s.LinearAssetProxyURL("org1", "https://uploads.linear.app/a"); err == nil {
		t.Fatal("missing public_base_url must be a loud error, not a silent bad URL")
	}
}
