package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"xolo/backend/internal/store"
)

// linearConfigured reports whether the Linear OAuth app is set up enough to connect.
func (s *Service) linearConfigured() bool {
	return s.cfg.Linear.ClientID != "" && s.cfg.Linear.ClientSecret != ""
}

// HandleLinearConnect redirects the browser to Linear's OAuth authorize page.
// actor=app installs the integration as a workspace-level app (actions are
// attributed to the app, not the connecting user), so this is a true workspace
// install rather than a per-user authorization.
func (s *Service) HandleLinearConnect(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() || !s.linearConfigured() {
		http.Error(w, "linear integration not configured", http.StatusServiceUnavailable)
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
		log.Printf("integrations: seal linear state: %v", err)
		http.Error(w, "failed to start linear connect", http.StatusInternalServerError)
		return
	}
	q := url.Values{}
	q.Set("client_id", s.cfg.Linear.ClientID)
	q.Set("redirect_uri", s.cfg.Linear.CallbackURL)
	q.Set("response_type", "code")
	// Linear takes a comma-separated scope list (e.g. "read,write").
	q.Set("scope", strings.Join(s.cfg.Linear.Scopes, ","))
	q.Set("state", state)
	// Workspace install uses actor=app (actions attributed to the app); a
	// user-level connect omits it so the token acts as the connecting user.
	if level == store.LevelWorkspace {
		q.Set("actor", "app")
	}
	http.Redirect(w, r, "https://linear.app/oauth/authorize?"+q.Encode(), http.StatusFound)
}

// HandleLinearCallback exchanges the OAuth code for an access token, looks up the
// workspace (organization) name via GraphQL, and stores the encrypted token plus
// workspace metadata for the org.
func (s *Service) HandleLinearCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		log.Printf("integrations: linear callback error: %s", e)
		http.Redirect(w, r, s.redirectAfter("linear", "error"), http.StatusFound)
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

	access, err := s.linearExchangeCode(r.Context(), code)
	if err != nil {
		log.Printf("integrations: linear exchange: %v", err)
		http.Redirect(w, r, s.redirectAfter("linear", "error"), http.StatusFound)
		return
	}

	// Best-effort: resolve the workspace name + id for display. A failure here
	// shouldn't block storing a working token.
	workspaceID, workspaceName := s.linearWorkspace(r.Context(), access.AccessToken)

	encToken, err := s.enc.Encrypt([]byte(access.AccessToken))
	if err != nil {
		log.Printf("integrations: encrypt linear token: %v", err)
		http.Error(w, "failed to secure linear token", http.StatusInternalServerError)
		return
	}

	in := store.Integration{
		OrgID:          st.OrgID,
		Provider:       store.ProviderLinear,
		Level:          store.LevelWorkspace,
		ExternalID:     workspaceID,
		EncryptedToken: encToken,
		ConnectedBy:    st.UserID,
		Metadata: map[string]any{
			"workspace_name": workspaceName,
			"scope":          access.Scope,
		},
	}
	if st.Level == string(store.LevelUser) {
		in.Level = store.LevelUser
		in.ConnectedUserID = st.UserID
	}
	if err := s.store.UpsertIntegration(r.Context(), in); err != nil {
		log.Printf("integrations: store linear token: %v", err)
		http.Error(w, "failed to save linear connection", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, s.redirectAfter("linear", "connected"), http.StatusFound)
}

// linearAccessResponse is the subset of the token response we use.
type linearAccessResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

// linearExchangeCode posts the code to Linear's token endpoint.
func (s *Service) linearExchangeCode(ctx context.Context, code string) (*linearAccessResponse, error) {
	form := url.Values{}
	form.Set("client_id", s.cfg.Linear.ClientID)
	form.Set("client_secret", s.cfg.Linear.ClientSecret)
	form.Set("redirect_uri", s.cfg.Linear.CallbackURL)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.linear.app/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linear token exchange: status %d", resp.StatusCode)
	}
	var out linearAccessResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("linear token exchange: empty access token")
	}
	return &out, nil
}

// linearWorkspace queries the Linear GraphQL API for the connected workspace's
// id and name. Best-effort: returns empty strings on any failure.
func (s *Service) linearWorkspace(ctx context.Context, token string) (id, name string) {
	const query = `{"query":"{ organization { id name } }"}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.linear.app/graphql", bytes.NewReader([]byte(query)))
	if err != nil {
		log.Printf("integrations: linear workspace request: %v", err)
		return "", ""
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("integrations: linear workspace query: %v", err)
		return "", ""
	}
	defer resp.Body.Close()

	var body struct {
		Data struct {
			Organization struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"organization"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		log.Printf("integrations: linear workspace decode: %v", err)
		return "", ""
	}
	return body.Data.Organization.ID, body.Data.Organization.Name
}

// LinearComment is the created comment returned by LinearCreateComment.
type LinearComment struct {
	ID string
}

// LinearCreateCommentInput describes a comment to create on a Linear issue.
// CreateAsUser + DisplayIconURL render the comment as "<name> (via <app>)" —
// native attribution available only to actor=app OAuth tokens (which is how the
// workspace Linear integration is installed). ParentID, when set, makes this a
// threaded reply under that comment.
type LinearCreateCommentInput struct {
	IssueID        string
	Body           string
	ParentID       string
	CreateAsUser   string
	DisplayIconURL string
}

// LinearCreateComment posts a comment via the commentCreate GraphQL mutation
// using the org's workspace token. The attribution fields (createAsUser,
// displayIconUrl) only take effect for actor=app tokens; Linear ignores them
// otherwise rather than erroring.
func (s *Service) LinearCreateComment(ctx context.Context, orgID string, in LinearCreateCommentInput) (LinearComment, error) {
	token, err := s.LinearAccessToken(ctx, orgID)
	if err != nil {
		return LinearComment{}, err
	}
	const mutation = `mutation($input: CommentCreateInput!) {
		commentCreate(input: $input) { success comment { id } }
	}`
	input := map[string]any{
		"issueId": in.IssueID,
		"body":    in.Body,
	}
	if in.ParentID != "" {
		input["parentId"] = in.ParentID
	}
	if in.CreateAsUser != "" {
		input["createAsUser"] = in.CreateAsUser
	}
	if in.DisplayIconURL != "" {
		input["displayIconUrl"] = in.DisplayIconURL
	}
	var resp struct {
		Data struct {
			CommentCreate struct {
				Success bool `json:"success"`
				Comment struct {
					ID string `json:"id"`
				} `json:"comment"`
			} `json:"commentCreate"`
		} `json:"data"`
	}
	if err := s.linearGraphQL(ctx, token, mutation, map[string]any{"input": input}, &resp); err != nil {
		return LinearComment{}, err
	}
	if !resp.Data.CommentCreate.Success {
		return LinearComment{}, fmt.Errorf("integrations: linear commentCreate returned success=false")
	}
	return LinearComment{ID: resp.Data.CommentCreate.Comment.ID}, nil
}

// LinearIssue is the subset of a Linear issue the sync engine needs (for
// channel naming/status checks).
type LinearIssue struct {
	ID         string
	Identifier string // e.g. "SKO-178"
	Title      string
	StateName  string // workflow state name (e.g. "Done")
}

// LinearIssueByID fetches an issue by id using the org's workspace token.
func (s *Service) LinearIssueByID(ctx context.Context, orgID, issueID string) (LinearIssue, error) {
	token, err := s.LinearAccessToken(ctx, orgID)
	if err != nil {
		return LinearIssue{}, err
	}
	const query = `query($id: String!) {
		issue(id: $id) { id identifier title state { name } }
	}`
	var resp struct {
		Data struct {
			Issue struct {
				ID         string `json:"id"`
				Identifier string `json:"identifier"`
				Title      string `json:"title"`
				State      struct {
					Name string `json:"name"`
				} `json:"state"`
			} `json:"issue"`
		} `json:"data"`
	}
	if err := s.linearGraphQL(ctx, token, query, map[string]any{"id": issueID}, &resp); err != nil {
		return LinearIssue{}, err
	}
	i := resp.Data.Issue
	return LinearIssue{ID: i.ID, Identifier: i.Identifier, Title: i.Title, StateName: i.State.Name}, nil
}

// linearGraphQL executes a GraphQL query/mutation against Linear's API with the
// given token and decodes the JSON response into out. It surfaces transport,
// HTTP-status, and GraphQL-level errors. Kept unexported: callers use the typed
// helpers above rather than issuing raw GraphQL.
func (s *Service) linearGraphQL(ctx context.Context, token, query string, variables map[string]any, out any) error {
	reqBody, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return fmt.Errorf("integrations: marshal linear request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.linear.app/graphql", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("integrations: linear graphql: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("integrations: linear graphql: status %d", resp.StatusCode)
	}
	// Decode into a wrapper that also captures top-level GraphQL errors, then
	// decode the data portion into the caller's out.
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("integrations: linear graphql: read body: %w", err)
	}
	var errCheck struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &errCheck); err == nil && len(errCheck.Errors) > 0 {
		return fmt.Errorf("integrations: linear graphql: %s", errCheck.Errors[0].Message)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("integrations: linear graphql: decode: %w", err)
	}
	return nil
}

// LinearAccessToken returns the decrypted workspace access token for an org's
// Linear connection.
func (s *Service) LinearAccessToken(ctx context.Context, orgID string) (string, error) {
	return s.linearToken(ctx, orgID, store.LevelWorkspace, "")
}

// LinearUserToken returns the decrypted per-user access token for a user's
// Linear connection.
func (s *Service) LinearUserToken(ctx context.Context, orgID, userID string) (string, error) {
	return s.linearToken(ctx, orgID, store.LevelUser, userID)
}

func (s *Service) linearToken(ctx context.Context, orgID string, level store.Level, userID string) (string, error) {
	in, err := s.store.GetIntegration(ctx, orgID, store.ProviderLinear, level, userID)
	if err != nil {
		return "", err
	}
	tok, err := s.enc.Decrypt(in.EncryptedToken)
	if err != nil {
		return "", err
	}
	return string(tok), nil
}
