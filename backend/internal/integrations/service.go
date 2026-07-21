// Package integrations connects a WorkOS organization to third-party providers
// (Slack workspaces, Linear workspaces). It owns the OAuth/installation
// redirect flows, persists the resulting installation/token in the store
// (tokens encrypted via crypto.Encryptor), and reports connection status.
//
// HTTP shape: the connect/callback endpoints are browser redirects (like the
// /auth/* routes) and live here as plain net/http handlers. The JSON status and
// disconnect endpoints are spec-driven and call into this service from the
// httpapi package.
package integrations

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"xolo/backend/internal/config"
	"xolo/backend/internal/crypto"
	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/slackapi"
	"xolo/backend/internal/store"
	"xolo/backend/internal/template"
)

// SessionResolver reads the active organization id and user id from the current
// request's session (empty when absent). It lets this package read the caller's
// identity without importing auth (avoiding an import cycle); the wiring passes
// an adapter over auth.UserFromContext in.
type SessionResolver func(r *http.Request) (orgID, userID string)

// Service orchestrates the integration flows. nil store/enc means integrations
// are not configured; handlers report that rather than panicking.
type Service struct {
	store   *store.Store
	enc     crypto.Encryptor
	cfg     config.Config
	resolve SessionResolver
	pub     pubsub.Publisher
	tmpl    template.Engine
	slack   slackapi.Client
}

// New builds the integrations service. store/enc may be nil when the app runs
// without a database; in that case Enabled() returns false. pub is the
// provider-agnostic publisher for integration events; pass pubsub.Nop to disable.
func New(st *store.Store, enc crypto.Encryptor, cfg config.Config, resolve SessionResolver, pub pubsub.Publisher) *Service {
	if pub == nil {
		pub = pubsub.Nop
	}
	return &Service{
		store:   st,
		enc:     enc,
		cfg:     cfg,
		resolve: resolve,
		pub:     pub,
		tmpl:    template.New(),
		slack:   slackapi.New(),
	}
}

// Enabled reports whether persistence (and thus integrations) is available.
func (s *Service) Enabled() bool { return s.store != nil && s.enc != nil }

// ProviderStatus is the connection state of one provider at one level (workspace
// or user) for an org.
type ProviderStatus struct {
	Provider    string         `json:"provider"`
	Level       string         `json:"level"` // "workspace" | "user"
	Connected   bool           `json:"connected"`
	Account     string         `json:"account,omitempty"` // Slack team / Linear workspace name
	ConnectedBy string         `json:"connectedBy,omitempty"`
	Metadata    map[string]any `json:"-"`
}

// providers is the canonical provider list, in display order. (GitHub is
// parked until phase 2.)
var providers = []store.Provider{store.ProviderSlack, store.ProviderLinear}

// Status returns, for each provider, both the org's workspace connection state
// and the given user's own (user-level) connection state. The result is a flat
// slice with one entry per (provider, level).
func (s *Service) Status(ctx context.Context, orgID, userID string) ([]ProviderStatus, error) {
	out := make([]ProviderStatus, 0, len(providers)*2)
	for _, p := range providers {
		out = append(out,
			ProviderStatus{Provider: string(p), Level: string(store.LevelWorkspace)},
			ProviderStatus{Provider: string(p), Level: string(store.LevelUser)},
		)
	}
	if !s.Enabled() || orgID == "" {
		return out, nil
	}

	workspaceRows, err := s.store.ListIntegrations(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var userRows []store.Integration
	if userID != "" {
		if userRows, err = s.store.ListUserIntegrations(ctx, orgID, userID); err != nil {
			return nil, err
		}
	}

	// Index by (provider, level) so we can fill the pre-seeded entries.
	type key struct {
		provider store.Provider
		level    store.Level
	}
	byKey := map[key]store.Integration{}
	for _, in := range workspaceRows {
		byKey[key{in.Provider, store.LevelWorkspace}] = in
	}
	for _, in := range userRows {
		byKey[key{in.Provider, store.LevelUser}] = in
	}
	for i := range out {
		k := key{store.Provider(out[i].Provider), store.Level(out[i].Level)}
		if in, ok := byKey[k]; ok {
			out[i].Connected = true
			out[i].ConnectedBy = in.ConnectedBy
			out[i].Account = accountLabel(in)
			out[i].Metadata = in.Metadata
		}
	}
	return out, nil
}

// Disconnect removes a provider integration at the given level. For the user
// level it deletes only the caller's own row (keyed by userID); for the
// workspace level it deletes the org-wide row.
func (s *Service) Disconnect(ctx context.Context, orgID, userID, provider, level string) error {
	if !s.Enabled() {
		return fmt.Errorf("integrations: not configured")
	}
	lvl := store.Level(level).Norm()
	uid := ""
	if lvl == store.LevelUser {
		uid = userID
	}
	return s.store.DeleteIntegration(ctx, orgID, store.Provider(provider), lvl, uid)
}

// redirectAfter builds the URL the browser returns to after a successful
// connect/callback, pointing at the SPA's integrations settings page with
// provider + status query flags so the UI can refresh and report the outcome.
func (s *Service) redirectAfter(provider, status string) string {
	base := s.cfg.App.PostLoginURL
	return fmt.Sprintf("%s/settings/integrations?provider=%s&status=%s", base, provider, status)
}

// Stable browser-error codes for /interrupted?code=… Never put raw exception
// text in the URL; title/message are fixed server copy.
const (
	ErrNotConfigured = "not_configured"
	ErrNoOrg         = "no_org"
	ErrStartFailed   = "start_failed"
	ErrOAuthDenied   = "oauth_denied"
	ErrInvalidState  = "invalid_state"
	ErrMissingCode   = "missing_code"
	ErrToken         = "token"
	ErrStore         = "store"
	ErrBillingLocked = "billing_locked"
)

func providerLabel(provider string) string {
	switch provider {
	case "slack":
		return "Slack"
	case "linear":
		return "Linear"
	case "":
		return "this integration"
	default:
		return provider
	}
}

// browserErrorCopy returns SPA title + detail for a stable code. Safe for URLs.
func browserErrorCopy(provider, code string) (title, message string) {
	name := providerLabel(provider)
	if code == ErrBillingLocked {
		title = "Unable to connect integrations"
	} else {
		title = "Unable to connect " + name
	}
	switch code {
	case ErrNotConfigured:
		message = name + " isn't set up on this server yet. Ask your admin to configure it."
	case ErrNoOrg:
		message = "Sign in with an organization before connecting " + name + "."
	case ErrStartFailed:
		message = "Couldn't start the " + name + " connection. Try again in a moment."
	case ErrOAuthDenied:
		message = "The " + name + " authorization was denied or cancelled."
	case ErrInvalidState:
		message = "This connection link expired or didn't match the browser that started it. Start again from Integrations."
	case ErrMissingCode:
		message = name + " didn't return an authorization code. Start the connection again."
	case ErrToken:
		message = "Couldn't secure the " + name + " token. Try connecting again."
	case ErrStore:
		message = "Connected to " + name + ", but saving failed. Try again."
	case ErrBillingLocked:
		message = "Your trial has ended. Subscribe to keep connecting integrations."
	default:
		message = "Something interrupted the " + name + " connection. Give it a moment and try again."
	}
	return title, message
}

// redirectErrorURL builds the SPA branded error page URL (NOT-37).
func (s *Service) redirectErrorURL(provider string, status int, code string) string {
	title, message := browserErrorCopy(provider, code)
	q := url.Values{}
	q.Set("status", strconv.Itoa(status))
	q.Set("code", code)
	q.Set("title", title)
	q.Set("message", message)
	if provider != "" {
		q.Set("provider", provider)
	}
	return s.cfg.App.PostLoginURL + "/interrupted?" + q.Encode()
}

// RedirectBrowserError 302s the browser to the SPA Quiet error page with
// status, stable code, and server-owned title/message. Prefer this over
// http.Error on connect/callback routes (NOT-32 / NOT-37).
func (s *Service) RedirectBrowserError(w http.ResponseWriter, r *http.Request, provider string, status int, code string) {
	http.Redirect(w, r, s.redirectErrorURL(provider, status, code), http.StatusFound)
}

// accountLabel derives a human label (Slack team / Linear workspace name) from
// the stored metadata.
func accountLabel(in store.Integration) string {
	for _, k := range []string{"team_name", "workspace_name", "login", "name"} {
		if v, ok := in.Metadata[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// --- OAuth state sealing -----------------------------------------------------
//
// The connect endpoints put an org id + random nonce into the OAuth `state`
// parameter, sealed with the Encryptor so it can't be forged or read, and verify
// it on the callback (CSRF protection + carrying the org through the redirect).
//
// Sealing alone is not enough: a sealed state is a bearer value the initiator
// can hand to anyone, so an attacker could start a connect for their own org
// and phish a victim into completing it, binding the victim's workspace token
// under the attacker's org. To prevent that, the connect endpoint also drops the
// nonce into an HttpOnly cookie on the initiating browser, and the callback
// requires the sealed state's nonce to match that cookie — so a callback
// completed by any browser other than the one that started the flow is rejected.
// IssuedAt additionally bounds the state's lifetime.

const oauthStateTTL = 10 * time.Minute

type oauthState struct {
	OrgID    string `json:"org"`
	UserID   string `json:"uid"`
	Level    string `json:"lvl,omitempty"` // "" or "workspace" = workspace; "user" = per-user
	Nonce    string `json:"n"`
	IssuedAt int64  `json:"iat"` // unix seconds; bounds the state's lifetime
}

// stateCookieName is the per-provider cookie that binds an OAuth flow to the
// browser that started it.
func stateCookieName(provider string) string { return "oauth_state_" + provider }

// setStateCookie records the flow's nonce on the initiating browser so the
// callback can prove it is the same browser. SameSite=Lax so it survives the
// provider's top-level redirect back to the callback.
func (s *Service) setStateCookie(w http.ResponseWriter, provider, nonce string) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName(provider),
		Value:    nonce,
		Path:     "/",
		HttpOnly: true,
		Secure:   !s.cfg.App.InsecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(oauthStateTTL / time.Second),
	})
}

func (s *Service) clearStateCookie(w http.ResponseWriter, provider string) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName(provider),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !s.cfg.App.InsecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// verifyState confirms the callback is completed by the browser that started
// the flow (nonce cookie matches the sealed state) and that the state is fresh.
func (s *Service) verifyState(r *http.Request, provider string, st oauthState) error {
	c, err := r.Cookie(stateCookieName(provider))
	if err != nil || c.Value == "" {
		return fmt.Errorf("missing oauth state cookie")
	}
	if subtle.ConstantTimeCompare([]byte(c.Value), []byte(st.Nonce)) != 1 {
		return fmt.Errorf("oauth state nonce mismatch")
	}
	if st.IssuedAt == 0 || time.Since(time.Unix(st.IssuedAt, 0)) > oauthStateTTL {
		return fmt.Errorf("oauth state expired")
	}
	return nil
}

func (s *Service) sealState(st oauthState) (string, error) {
	if st.IssuedAt == 0 {
		st.IssuedAt = time.Now().Unix()
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	ct, err := s.enc.Encrypt(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(ct), nil
}

// newNonce returns a random URL-safe nonce for the sealed OAuth state.
func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) openState(encoded string) (oauthState, error) {
	var st oauthState
	ct, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return st, err
	}
	raw, err := s.enc.Decrypt(ct)
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(raw, &st)
	return st, err
}
