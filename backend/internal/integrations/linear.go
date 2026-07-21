package integrations

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xolo/backend/internal/slackapi"
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
		s.RedirectBrowserError(w, r, "linear")
		return
	}
	orgID, userID := s.resolve(r)
	if orgID == "" {
		s.RedirectBrowserError(w, r, "linear")
		return
	}
	level := store.LevelWorkspace
	if r.URL.Query().Get("level") == string(store.LevelUser) {
		level = store.LevelUser
	}
	nonce := newNonce()
	state, err := s.sealState(oauthState{OrgID: orgID, UserID: userID, Level: string(level), Nonce: nonce})
	if err != nil {
		slog.ErrorContext(r.Context(), "integrations: seal linear state", "error", err)
		s.RedirectBrowserError(w, r, "linear")
		return
	}
	// Bind this flow to the initiating browser; the callback requires the cookie
	// to match the sealed state's nonce.
	s.setStateCookie(w, "linear", nonce)
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
		slog.WarnContext(r.Context(), "integrations: linear callback error", "error", e)
		s.RedirectBrowserError(w, r, "linear")
		return
	}
	st, err := s.openState(q.Get("state"))
	if err != nil || st.OrgID == "" {
		s.RedirectBrowserError(w, r, "linear")
		return
	}
	// Bind to the initiating browser: reject a callback whose state nonce does
	// not match the cookie set at connect (OAuth account-linking CSRF), or whose
	// state has expired.
	if err := s.verifyState(r, "linear", st); err != nil {
		slog.WarnContext(r.Context(), "integrations: linear oauth state binding failed", "error", err)
		s.RedirectBrowserError(w, r, "linear")
		return
	}
	s.clearStateCookie(w, "linear")
	code := q.Get("code")
	if code == "" {
		s.RedirectBrowserError(w, r, "linear")
		return
	}

	access, err := s.linearExchangeCode(r.Context(), code)
	if err != nil {
		slog.ErrorContext(r.Context(), "integrations: linear exchange", "error", err)
		s.RedirectBrowserError(w, r, "linear")
		return
	}

	// Best-effort: resolve the workspace name + id for display. A failure here
	// shouldn't block storing a working token.
	workspaceID, workspaceName := s.linearWorkspace(r.Context(), access.AccessToken)

	encToken, err := s.enc.Encrypt([]byte(access.AccessToken))
	if err != nil {
		slog.ErrorContext(r.Context(), "integrations: encrypt linear token", "error", err)
		s.RedirectBrowserError(w, r, "linear")
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
		slog.ErrorContext(r.Context(), "integrations: store linear token", "error", err)
		s.RedirectBrowserError(w, r, "linear")
		return
	}

	// Best-effort: sync the workspace's team workflow states so the settings UI
	// has real status options immediately. Only for a workspace install (the app
	// token can read every team). Runs detached — the request context ends at the
	// redirect below, so we use a fresh bounded context and never block the user.
	if in.Level == store.LevelWorkspace {
		orgID := st.OrgID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.SyncLinearTeamStates(ctx, orgID); err != nil {
				slog.ErrorContext(ctx, "integrations: initial linear team-state sync", "org_id", orgID, "error", err)
			}
		}()
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

	// Read the body once so we can surface Linear's error detail on a non-200
	// (e.g. redirect_uri mismatch, invalid_grant) rather than just the status.
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, fmt.Errorf("linear token exchange: read body: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linear token exchange: status %d: %s", resp.StatusCode, string(body))
	}
	var out linearAccessResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("linear token exchange: decode: %w (body: %s)", err, string(body))
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("linear token exchange: empty access token (body: %s)", string(body))
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
		slog.ErrorContext(ctx, "integrations: linear workspace request", "error", err)
		return "", ""
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.ErrorContext(ctx, "integrations: linear workspace query", "error", err)
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
		slog.ErrorContext(ctx, "integrations: linear workspace decode", "error", err)
		return "", ""
	}
	return body.Data.Organization.ID, body.Data.Organization.Name
}

// LinearComment is the created comment returned by LinearCreateComment.
type LinearComment struct {
	ID string
}

// LinearCreateCommentInput describes a comment to create on a Linear issue.
// SlackAuthorID, when set, is the Slack user id of the message author: the
// comment is posted with that person's own linked Linear token when their
// identity is connected, and app-level (the org's actor=app token, authored by
// the app itself) when it is not. We never post as anyone who didn't authorize
// it. ParentID, when set, makes this a threaded reply under that comment.
type LinearCreateCommentInput struct {
	IssueID       string
	Body          string
	ParentID      string
	SlackAuthorID string
	// AuthorDisplayName is the author's Slack display name, used only for the
	// plain-text provenance byline on app-level posts (unlinked identity).
	AuthorDisplayName string
	// Attachments are files mirrored with the comment. Each is uploaded to
	// Linear's file storage (with the same token that posts the comment) and
	// embedded in the body as markdown.
	Attachments []LinearCommentAttachment
}

// LinearCommentAttachment is one file to upload alongside a mirrored comment.
type LinearCommentAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// LinearCreateComment posts a comment via the commentCreate GraphQL mutation.
// Token selection: the author's own user-level Linear token when
// SlackAuthorID resolves to a connected identity, otherwise the org's
// actor=app workspace token (the comment is then authored by the app —
// explicitly app-level, never another user's credentials).
func (s *Service) LinearCreateComment(ctx context.Context, orgID string, in LinearCreateCommentInput) (LinearComment, error) {
	var token string
	byline := ""
	if in.SlackAuthorID != "" {
		uid, err := s.store.UserIDBySlackUserID(ctx, orgID, in.SlackAuthorID)
		switch {
		case err == nil:
			t, terr := s.LinearUserToken(ctx, orgID, uid)
			switch {
			case terr == nil:
				token = t
			case !errors.Is(terr, store.ErrNotFound):
				return LinearComment{}, terr // transient; caller retries
			}
		case !errors.Is(err, store.ErrNotFound):
			return LinearComment{}, err // transient; caller retries
		}
		if token == "" {
			slog.InfoContext(ctx, "integrations: slack author has no linked linear identity; posting app-level",
				"org_id", orgID, "slack_user_id", in.SlackAuthorID)
			// Plain-text provenance so readers still know who spoke — this is
			// a byline on an app-authored comment, not impersonation. Appended
			// after the attachment embeds so it stays the comment's last line.
			if in.AuthorDisplayName != "" {
				byline = "\n\n— " + in.AuthorDisplayName + " on Slack"
			} else {
				byline = "\n\n— posted from Slack"
			}
		}
	}
	if token == "" {
		t, err := s.LinearAccessToken(ctx, orgID)
		if err != nil {
			return LinearComment{}, err
		}
		token = t
	}

	// Upload attachments with the same token that authors the comment, and
	// embed each as markdown (images render inline in Linear). A failed upload
	// degrades to a note instead of failing the comment: an unuploadable file
	// would otherwise nack and redeliver forever, and the text still matters.
	for _, att := range in.Attachments {
		assetURL, err := s.linearUploadFile(ctx, token, att)
		if err != nil {
			slog.ErrorContext(ctx, "integrations: linear file upload failed",
				"org_id", orgID, "filename", att.Filename, "error", err)
			in.Body += fmt.Sprintf("\n\n_(attachment %q could not be synced)_", att.Filename)
			continue
		}
		name := mdLinkText(att.Filename)
		if strings.HasPrefix(att.ContentType, "image/") {
			in.Body += fmt.Sprintf("\n\n![%s](%s)", name, assetURL)
		} else {
			in.Body += fmt.Sprintf("\n\n[%s](%s)", name, assetURL)
		}
	}
	in.Body = strings.TrimSpace(in.Body + byline)
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

// mdLinkText sanitizes a filename for use as markdown link text so a bracketed
// name can't break out of the link syntax.
func mdLinkText(name string) string {
	return strings.NewReplacer("[", "(", "]", ")", "\n", " ").Replace(name)
}

// linearUploadFile reserves storage via the fileUpload mutation, PUTs the bytes
// to the returned signed URL with the headers Linear requires, and returns the
// asset URL to embed in a comment body.
func (s *Service) linearUploadFile(ctx context.Context, token string, att LinearCommentAttachment) (string, error) {
	const mutation = `mutation($contentType: String!, $filename: String!, $size: Int!) {
		fileUpload(contentType: $contentType, filename: $filename, size: $size) {
			success
			uploadFile { uploadUrl assetUrl headers { key value } }
		}
	}`
	contentType := att.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	var resp struct {
		Data struct {
			FileUpload struct {
				Success    bool `json:"success"`
				UploadFile struct {
					UploadURL string `json:"uploadUrl"`
					AssetURL  string `json:"assetUrl"`
					Headers   []struct {
						Key   string `json:"key"`
						Value string `json:"value"`
					} `json:"headers"`
				} `json:"uploadFile"`
			} `json:"fileUpload"`
		} `json:"data"`
	}
	vars := map[string]any{"contentType": contentType, "filename": att.Filename, "size": len(att.Data)}
	if err := s.linearGraphQL(ctx, token, mutation, vars, &resp); err != nil {
		return "", err
	}
	up := resp.Data.FileUpload.UploadFile
	if !resp.Data.FileUpload.Success || up.UploadURL == "" {
		return "", fmt.Errorf("integrations: linear fileUpload returned no upload url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, up.UploadURL, bytes.NewReader(att.Data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	for _, h := range up.Headers {
		req.Header.Set(h.Key, h.Value)
	}
	putResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("integrations: linear file put: %w", err)
	}
	defer putResp.Body.Close()
	io.Copy(io.Discard, putResp.Body)
	if putResp.StatusCode < 200 || putResp.StatusCode > 299 {
		return "", fmt.Errorf("integrations: linear file put: status %d", putResp.StatusCode)
	}
	return up.AssetURL, nil
}

// LinearFileDownload fetches a private Linear upload (uploads.linear.app) with
// the org's workspace token and returns its bytes and content type. Used to
// re-host comment attachments in Slack, since Slack can't render URLs that
// require Linear auth.
func (s *Service) LinearFileDownload(ctx context.Context, orgID, fileURL string) ([]byte, string, error) {
	token, err := s.LinearAccessToken(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("integrations: linear file download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("integrations: linear file download: status %d", resp.StatusCode)
	}
	// Read one byte past the cap so an oversized file errors rather than being
	// silently truncated into a corrupt mirror.
	data, err := io.ReadAll(io.LimitReader(resp.Body, slackapi.MaxFileBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("integrations: linear file download: read: %w", err)
	}
	if len(data) > slackapi.MaxFileBytes {
		return nil, "", fmt.Errorf("integrations: linear file download: exceeds %d byte cap", slackapi.MaxFileBytes)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// assetProxyPayload is what a signed asset-proxy token decrypts to.
type assetProxyPayload struct {
	OrgID   string `json:"o"`
	FileURL string `json:"u"`
	// Exp is the unix-seconds expiry: long enough for Slack's one-time
	// server-side fetch, short enough that a leaked URL is dead on arrival.
	Exp int64 `json:"e"`
}

// assetProxyTTL bounds how long a minted asset URL keeps working. Slack's
// image proxy fetches the bytes once within seconds of the post and serves
// its own cached copy afterwards, so the URL only needs to survive that first
// fetch — 5 minutes (GitHub uses the same window for private attachments).
// The accepted tail risk: if Slack ever evicts its cache and re-fetches, the
// image in old history breaks. Grafting updates re-mint fresh tokens.
const assetProxyTTL = 5 * time.Minute

// LinearAssetProxyURL builds a public, signed URL on our backend that streams
// the given private Linear upload. Embedded as image_url in Slack blocks:
// Slack's image proxy can fetch it, unlike uploads.linear.app (Linear auth) or
// an unshared Slack file (per-viewer access). The token is AEAD-sealed, so the
// URL is unguessable and tamper-proof, but anyone holding it can fetch the
// asset — the same trust model as a Slack file link.
func (s *Service) LinearAssetProxyURL(orgID, fileURL string) (string, error) {
	base := strings.TrimRight(s.cfg.Server.PublicBaseURL, "/")
	if base == "" {
		return "", fmt.Errorf("integrations: server.public_base_url not configured; cannot build asset proxy URL")
	}
	raw, err := json.Marshal(assetProxyPayload{
		OrgID:   orgID,
		FileURL: fileURL,
		Exp:     time.Now().Add(assetProxyTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	sealed, err := s.enc.Encrypt(raw)
	if err != nil {
		return "", fmt.Errorf("integrations: seal asset token: %w", err)
	}
	return base + "/integrations/linear/asset/" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// openAssetProxyToken decrypts and validates an asset-proxy token: intact
// seal, allowed host, unexpired. Every failure is generic — callers 404.
func (s *Service) openAssetProxyToken(token string) (assetProxyPayload, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return assetProxyPayload{}, fmt.Errorf("bad encoding: %w", err)
	}
	raw, err := s.enc.Decrypt(sealed)
	if err != nil {
		return assetProxyPayload{}, fmt.Errorf("bad seal: %w", err)
	}
	var p assetProxyPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return assetProxyPayload{}, fmt.Errorf("bad payload: %w", err)
	}
	// Only Linear's upload host may be proxied — the sealed payload should
	// never contain anything else, but fail closed regardless.
	u, err := url.Parse(p.FileURL)
	if err != nil || u.Scheme != "https" || u.Host != "uploads.linear.app" {
		return assetProxyPayload{}, fmt.Errorf("disallowed target")
	}
	// Fail closed on missing expiry too: every minted token carries one.
	if p.Exp <= 0 || time.Now().Unix() > p.Exp {
		return assetProxyPayload{}, fmt.Errorf("expired")
	}
	return p, nil
}

// HandleLinearAssetProxy streams a private Linear upload identified by a
// signed token (see LinearAssetProxyURL). Invalid or tampered tokens 404;
// nothing about the failure is echoed back.
func (s *Service) HandleLinearAssetProxy(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		http.NotFound(w, r)
		return
	}
	p, err := s.openAssetProxyToken(r.PathValue("token"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	token, err := s.LinearAccessToken(r.Context(), p.OrgID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, p.FileURL, nil)
	if err != nil {
		http.Error(w, "proxy failed", http.StatusBadGateway)
		return
	}
	req.Header.Set("Authorization", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "proxy failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "proxy failed", http.StatusBadGateway)
		return
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	// The asset is immutable (Linear upload URLs are content-addressed), so
	// let Slack's image proxy cache it hard.
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	if _, err := io.Copy(w, io.LimitReader(resp.Body, slackapi.MaxFileBytes)); err != nil {
		slog.WarnContext(r.Context(), "integrations: asset proxy stream interrupted", "error", err)
	}
}

// LinearIssue is the subset of a Linear issue the sync engine needs (for
// channel naming/status checks and resolving which team's config applies).
type LinearIssue struct {
	ID         string
	Identifier string // e.g. "SKO-178"
	Title      string
	StateName  string // workflow state name (e.g. "Done")
	TeamID     string // owning team's id (resolves the applicable config)
}

// LinearIssueByID fetches an issue by id using the org's workspace token.
func (s *Service) LinearIssueByID(ctx context.Context, orgID, issueID string) (LinearIssue, error) {
	token, err := s.LinearAccessToken(ctx, orgID)
	if err != nil {
		return LinearIssue{}, err
	}
	const query = `query($id: String!) {
		issue(id: $id) { id identifier title state { name } team { id } }
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
				Team struct {
					ID string `json:"id"`
				} `json:"team"`
			} `json:"issue"`
		} `json:"data"`
	}
	if err := s.linearGraphQL(ctx, token, query, map[string]any{"id": issueID}, &resp); err != nil {
		return LinearIssue{}, err
	}
	i := resp.Data.Issue
	return LinearIssue{ID: i.ID, Identifier: i.Identifier, Title: i.Title, StateName: i.State.Name, TeamID: i.Team.ID}, nil
}

// LinearWorkflowState is one workflow state (issue status) of a team.
type LinearWorkflowState struct {
	ID       string
	Name     string
	Type     string
	Color    string
	Position float64
}

// LinearTeamStates is a team plus its workflow states, as fetched from Linear.
type LinearTeamStates struct {
	TeamID   string
	TeamKey  string
	TeamName string
	States   []LinearWorkflowState
}

// LinearTeamStates fetches every team in the workspace with its workflow states
// using the org's workspace token. Used to sync the status options for the
// settings UI. Linear caps page size; 250 comfortably covers a workspace's teams
// and each team's states (both are small sets in practice).
func (s *Service) LinearTeamStates(ctx context.Context, orgID string) ([]LinearTeamStates, error) {
	token, err := s.LinearAccessToken(ctx, orgID)
	if err != nil {
		return nil, err
	}
	// Keep page sizes modest: Linear enforces a query-complexity budget and
	// rejects overly broad nested pagination with a 400. A workspace's teams and
	// each team's states are small sets, so these bounds are comfortable while
	// staying well under the complexity ceiling.
	const query = `query {
		teams(first: 100) {
			nodes {
				id key name
				states(first: 50) { nodes { id name type color position } }
			}
		}
	}`
	var resp struct {
		Data struct {
			Teams struct {
				Nodes []struct {
					ID     string `json:"id"`
					Key    string `json:"key"`
					Name   string `json:"name"`
					States struct {
						Nodes []struct {
							ID       string  `json:"id"`
							Name     string  `json:"name"`
							Type     string  `json:"type"`
							Color    string  `json:"color"`
							Position float64 `json:"position"`
						} `json:"nodes"`
					} `json:"states"`
				} `json:"nodes"`
			} `json:"teams"`
		} `json:"data"`
	}
	if err := s.linearGraphQL(ctx, token, query, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]LinearTeamStates, 0, len(resp.Data.Teams.Nodes))
	for _, t := range resp.Data.Teams.Nodes {
		states := make([]LinearWorkflowState, 0, len(t.States.Nodes))
		for _, st := range t.States.Nodes {
			states = append(states, LinearWorkflowState{
				ID: st.ID, Name: st.Name, Type: st.Type, Color: st.Color, Position: st.Position,
			})
		}
		out = append(out, LinearTeamStates{
			TeamID: t.ID, TeamKey: t.Key, TeamName: t.Name, States: states,
		})
	}
	return out, nil
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
	// Read the body once; on a non-200, Linear returns a JSON error body that
	// names the offending field/arg, so include it rather than just the status.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("integrations: linear graphql: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("integrations: linear graphql: status %d: %s", resp.StatusCode, string(raw))
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
