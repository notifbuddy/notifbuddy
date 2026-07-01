package integrations

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"xolo/backend/internal/store"
)

// githubConfigured reports whether the GitHub App is set up enough to connect.
func (s *Service) githubConfigured() bool {
	return s.cfg.GitHub.AppSlug != "" && s.cfg.GitHub.AppID != "" && s.cfg.GitHub.PrivateKey != ""
}

// HandleGitHubConnect redirects the browser to the GitHub App installation page,
// carrying a sealed `state` (org + nonce) that the callback verifies.
func (s *Service) HandleGitHubConnect(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		http.Error(w, "github integration not configured", http.StatusServiceUnavailable)
		return
	}
	orgID, userID := s.resolve(r)
	if orgID == "" {
		http.Error(w, "no active organization", http.StatusUnauthorized)
		return
	}
	// User-level connections use the user-to-server OAuth flow, not App install.
	reqLevel := r.URL.Query().Get("level")
	log.Printf("integrations: github connect: raw_query=%q level=%q -> %s flow",
		r.URL.RawQuery, reqLevel, map[bool]string{true: "user", false: "workspace"}[reqLevel == string(store.LevelUser)])
	if reqLevel == string(store.LevelUser) {
		s.handleGitHubUserConnect(w, r, orgID, userID)
		return
	}
	if !s.githubConfigured() {
		http.Error(w, "github integration not configured", http.StatusServiceUnavailable)
		return
	}
	state, err := s.sealState(oauthState{OrgID: orgID, UserID: userID, Nonce: newNonce()})
	if err != nil {
		log.Printf("integrations: seal github state: %v", err)
		http.Error(w, "failed to start github connect", http.StatusInternalServerError)
		return
	}
	// installations/new lets the user pick the account + repos. GitHub appends
	// installation_id, setup_action, and our state to the callback URL.
	u := fmt.Sprintf("https://github.com/apps/%s/installations/new?state=%s",
		url.PathEscape(s.cfg.GitHub.AppSlug), url.QueryEscape(state))
	http.Redirect(w, r, u, http.StatusFound)
}

// HandleGitHubCallback receives the post-installation redirect. It verifies the
// sealed state, then stores the installation for the org. We persist the
// installation_id (not a token) and mint short-lived installation tokens on
// demand via the App JWT.
func (s *Service) HandleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	st, err := s.openState(q.Get("state"))
	if err != nil || st.OrgID == "" {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	// User-level callbacks complete the user-to-server OAuth flow.
	log.Printf("integrations: github callback: raw_query=%q state.level=%q -> %s flow",
		r.URL.RawQuery, st.Level, map[bool]string{true: "user", false: "workspace"}[st.Level == string(store.LevelUser)])
	if st.Level == string(store.LevelUser) {
		s.handleGitHubUserCallback(w, r, st)
		return
	}
	setupAction := q.Get("setup_action")
	installationID := q.Get("installation_id")

	// "request" means the install needs org-admin approval; there's no
	// installation yet. Send the user back with a flag so the UI can explain.
	if setupAction == "request" || installationID == "" {
		http.Redirect(w, r, s.redirectAfter("github", "pending"), http.StatusFound)
		return
	}

	// Fetch the installation's account login for display (best-effort).
	login := s.githubInstallationAccount(r.Context(), installationID)

	err = s.store.UpsertIntegration(r.Context(), store.Integration{
		OrgID:       st.OrgID,
		Provider:    store.ProviderGitHub,
		ExternalID:  installationID,
		ConnectedBy: st.UserID,
		Metadata: map[string]any{
			"account_login": login,
			"setup_action":  setupAction,
		},
	})
	if err != nil {
		log.Printf("integrations: store github installation: %v", err)
		http.Error(w, "failed to save github installation", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.redirectAfter("github", "connected"), http.StatusFound)
}

// InstallationToken mints a short-lived GitHub installation access token for the
// org's stored installation. Tokens are not persisted — they're minted on demand
// from the App JWT, which is the GitHub-recommended pattern.
func (s *Service) InstallationToken(ctx context.Context, orgID string) (string, error) {
	in, err := s.store.GetIntegration(ctx, orgID, store.ProviderGitHub, store.LevelWorkspace, "")
	if err != nil {
		return "", err
	}
	appJWT, err := s.githubAppJWT()
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", in.ExternalID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("integrations: github token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("integrations: github token status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// githubAppJWT builds a short-lived (<=10 min) JWT signed with the App's private
// key, authenticating *as the App* (used to mint installation tokens).
func (s *Service) githubAppJWT() (string, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(s.cfg.GitHub.PrivateKey))
	if err != nil {
		return "", fmt.Errorf("integrations: parse github private key: %w", err)
	}
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": now.Add(-30 * time.Second).Unix(), // small backdate for clock skew
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": s.cfg.GitHub.AppID,
	})
	return tok.SignedString(key)
}

// githubInstallationAccount fetches the account login for an installation,
// best-effort (returns "" on any error so connect still succeeds).
func (s *Service) githubInstallationAccount(ctx context.Context, installationID string) string {
	appJWT, err := s.githubAppJWT()
	if err != nil {
		return ""
	}
	url := "https://api.github.com/app/installations/" + installationID
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return ""
	}
	return out.Account.Login
}

func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
