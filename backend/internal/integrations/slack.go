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

// slackConfigured reports whether the Slack app is set up enough to connect.
func (s *Service) slackConfigured() bool {
	return s.cfg.Slack.ClientID != "" && s.cfg.Slack.ClientSecret != ""
}

// HandleSlackConnect redirects the browser to Slack's OAuth v2 authorize page
// with the configured bot scopes and a sealed state.
func (s *Service) HandleSlackConnect(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() || !s.slackConfigured() {
		http.Error(w, "slack integration not configured", http.StatusServiceUnavailable)
		return
	}
	orgID, userID := s.resolve(r)
	if orgID == "" {
		http.Error(w, "no active organization", http.StatusUnauthorized)
		return
	}
	level := store.LevelWorkspace
	if r.URL.Query().Get("level") == string(store.LevelUser) {
		level = store.LevelUser
	}
	state, err := s.sealState(oauthState{OrgID: orgID, UserID: userID, Level: string(level), Nonce: newNonce()})
	if err != nil {
		log.Printf("integrations: seal slack state: %v", err)
		http.Error(w, "failed to start slack connect", http.StatusInternalServerError)
		return
	}
	q := url.Values{}
	q.Set("client_id", s.cfg.Slack.ClientID)
	q.Set("redirect_uri", s.cfg.Slack.CallbackURL)
	q.Set("state", state)
	// Slack splits bot scopes (scope) from user scopes (user_scope). A user-level
	// connect requests only user scopes (the xoxp token under authed_user); a
	// workspace connect requests the bot scopes.
	if level == store.LevelUser {
		q.Set("user_scope", strings.Join(s.cfg.Slack.UserScopes, " "))
	} else {
		q.Set("scope", strings.Join(s.cfg.Slack.Scopes, " "))
	}
	http.Redirect(w, r, "https://slack.com/oauth/v2/authorize?"+q.Encode(), http.StatusFound)
}

// HandleSlackCallback exchanges the OAuth code for a bot token at
// oauth.v2.access, then stores the team id + (encrypted) bot token for the org.
func (s *Service) HandleSlackCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		log.Printf("integrations: slack callback error: %s", e)
		http.Redirect(w, r, s.redirectAfter("slack", "error"), http.StatusFound)
		return
	}
	st, err := s.openState(q.Get("state"))
	if err != nil || st.OrgID == "" {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	code := q.Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	access, err := s.slackExchangeCode(r.Context(), code)
	if err != nil {
		log.Printf("integrations: slack exchange: %v", err)
		http.Redirect(w, r, s.redirectAfter("slack", "error"), http.StatusFound)
		return
	}

	// Pick the token + level. A user-level connect stores the per-user token
	// (xoxp) from authed_user; a workspace connect stores the bot token (xoxb).
	in := store.Integration{
		OrgID:       st.OrgID,
		Provider:    store.ProviderSlack,
		ExternalID:  access.Team.ID,
		ConnectedBy: st.UserID,
	}
	var rawToken string
	if st.Level == string(store.LevelUser) {
		if access.AuthedUser.AccessToken == "" {
			log.Printf("integrations: slack user callback missing authed_user token")
			http.Redirect(w, r, s.redirectAfter("slack", "error"), http.StatusFound)
			return
		}
		in.Level = store.LevelUser
		in.ConnectedUserID = st.UserID
		rawToken = access.AuthedUser.AccessToken
		in.Metadata = map[string]any{
			"team_name":     access.Team.Name,
			"scope":         access.AuthedUser.Scope,
			"slack_user_id": access.AuthedUser.ID,
		}
	} else {
		in.Level = store.LevelWorkspace
		rawToken = access.AccessToken
		in.Metadata = map[string]any{
			"team_name": access.Team.Name,
			"scope":     access.Scope,
			"bot_user":  access.BotUserID,
		}
	}

	// The token is the sensitive value; encrypt it before storing.
	encToken, err := s.enc.Encrypt([]byte(rawToken))
	if err != nil {
		log.Printf("integrations: encrypt slack token: %v", err)
		http.Error(w, "failed to secure slack token", http.StatusInternalServerError)
		return
	}
	in.EncryptedToken = encToken

	if err := s.store.UpsertIntegration(r.Context(), in); err != nil {
		log.Printf("integrations: store slack token: %v", err)
		http.Error(w, "failed to save slack connection", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.redirectAfter("slack", "connected"), http.StatusFound)
}

// slackAccessResponse is the subset of oauth.v2.access we use. The top-level
// access_token is the bot token (xoxb); a user token (xoxp) and its scopes live
// under authed_user when user scopes were requested.
type slackAccessResponse struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error"`
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	BotUserID   string `json:"bot_user_id"`
	Team        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"team"`
	AuthedUser struct {
		ID          string `json:"id"`
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	} `json:"authed_user"`
}

// slackExchangeCode posts the code to oauth.v2.access with the app credentials.
func (s *Service) slackExchangeCode(ctx context.Context, code string) (*slackAccessResponse, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("redirect_uri", s.cfg.Slack.CallbackURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://slack.com/api/oauth.v2.access", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(s.cfg.Slack.ClientID, s.cfg.Slack.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out slackAccessResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("slack oauth.v2.access: %s", out.Error)
	}
	return &out, nil
}

// SlackBotToken returns the decrypted workspace bot token for an org's Slack
// connection.
func (s *Service) SlackBotToken(ctx context.Context, orgID string) (string, error) {
	return s.slackToken(ctx, orgID, store.LevelWorkspace, "")
}

// SlackUserToken returns the decrypted per-user token for a user's Slack
// connection.
func (s *Service) SlackUserToken(ctx context.Context, orgID, userID string) (string, error) {
	return s.slackToken(ctx, orgID, store.LevelUser, userID)
}

func (s *Service) slackToken(ctx context.Context, orgID string, level store.Level, userID string) (string, error) {
	in, err := s.store.GetIntegration(ctx, orgID, store.ProviderSlack, level, userID)
	if err != nil {
		return "", err
	}
	tok, err := s.enc.Decrypt(in.EncryptedToken)
	if err != nil {
		return "", err
	}
	return string(tok), nil
}
