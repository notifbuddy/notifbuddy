package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"xolo/backend/internal/store"
)

// GitHub user-level connection (user-to-server OAuth).
//
// This is a different flow from the App *installation* (workspace level): instead
// of installing the App on an org, the user authorizes the App to act on their
// behalf, producing a user access token (ghu_...). We store it encrypted as a
// level=user row keyed by the connecting user. Used for two-way sync where
// activity must be attributed to the user.

// handleGitHubUserConnect redirects to GitHub's user authorization page.
func (s *Service) handleGitHubUserConnect(w http.ResponseWriter, r *http.Request, orgID, userID string) {
	// User-to-server OAuth needs the App's OAuth client id/secret.
	if s.cfg.GitHub.ClientID == "" || s.cfg.GitHub.ClientSecret == "" {
		http.Error(w, "github user integration not configured", http.StatusServiceUnavailable)
		return
	}
	state, err := s.sealState(oauthState{OrgID: orgID, UserID: userID, Level: string(store.LevelUser), Nonce: newNonce()})
	if err != nil {
		log.Printf("integrations: seal github user state: %v", err)
		http.Error(w, "failed to start github connect", http.StatusInternalServerError)
		return
	}
	q := url.Values{}
	q.Set("client_id", s.cfg.GitHub.ClientID)
	q.Set("redirect_uri", s.githubUserCallbackURL())
	q.Set("state", state)
	if scopes := s.cfg.GitHub.UserScopes; len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

// handleGitHubUserCallback exchanges the code for a user access token and stores
// it as the user-level row.
func (s *Service) handleGitHubUserCallback(w http.ResponseWriter, r *http.Request, st oauthState) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		log.Printf("integrations: github user callback error: %s", e)
		http.Redirect(w, r, s.redirectAfter("github", "error"), http.StatusFound)
		return
	}
	code := q.Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	access, login, err := s.githubExchangeUserCode(r.Context(), code)
	if err != nil {
		log.Printf("integrations: github user exchange: %v", err)
		http.Redirect(w, r, s.redirectAfter("github", "error"), http.StatusFound)
		return
	}

	encToken, err := s.enc.Encrypt([]byte(access))
	if err != nil {
		log.Printf("integrations: encrypt github user token: %v", err)
		http.Error(w, "failed to secure github token", http.StatusInternalServerError)
		return
	}

	err = s.store.UpsertIntegration(r.Context(), store.Integration{
		OrgID:           st.OrgID,
		Provider:        store.ProviderGitHub,
		Level:           store.LevelUser,
		ConnectedUserID: st.UserID,
		ExternalID:      login,
		EncryptedToken:  encToken,
		ConnectedBy:     st.UserID,
		Metadata:        map[string]any{"account_login": login},
	})
	if err != nil {
		log.Printf("integrations: store github user token: %v", err)
		http.Error(w, "failed to save github connection", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.redirectAfter("github", "connected"), http.StatusFound)
}

// githubExchangeUserCode posts the code to GitHub's token endpoint and returns
// the user access token plus the authenticated user's login (best-effort).
func (s *Service) githubExchangeUserCode(ctx context.Context, code string) (token, login string, err error) {
	form := url.Values{}
	form.Set("client_id", s.cfg.GitHub.ClientID)
	form.Set("client_secret", s.cfg.GitHub.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", s.githubUserCallbackURL())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	if out.AccessToken == "" {
		return "", "", fmt.Errorf("github user token exchange: %s", out.Error)
	}
	return out.AccessToken, s.githubUserLogin(ctx, out.AccessToken), nil
}

// githubUserLogin fetches the authenticated user's login for display
// (best-effort: returns "" on any failure).
func (s *Service) githubUserLogin(ctx context.Context, token string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
		Login string `json:"login"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return ""
	}
	return out.Login
}

// githubUserCallbackURL is the redirect for the user-to-server flow, defaulting
// to the App callback's host with a /user suffix when unset.
func (s *Service) githubUserCallbackURL() string {
	if s.cfg.GitHub.UserCallbackURL != "" {
		return s.cfg.GitHub.UserCallbackURL
	}
	return s.cfg.GitHub.CallbackURL
}

// GitHubUserToken returns the decrypted user access token for a user's GitHub
// connection.
func (s *Service) GitHubUserToken(ctx context.Context, orgID, userID string) (string, error) {
	in, err := s.store.GetIntegration(ctx, orgID, store.ProviderGitHub, store.LevelUser, userID)
	if err != nil {
		return "", err
	}
	tok, err := s.enc.Decrypt(in.EncryptedToken)
	if err != nil {
		return "", err
	}
	return string(tok), nil
}
