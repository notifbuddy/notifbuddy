// Package integrations connects a WorkOS organization to third-party providers
// (GitHub App installations, Slack workspaces). It owns the OAuth/installation
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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"xolo/backend/internal/config"
	"xolo/backend/internal/crypto"
	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/store"
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
}

// New builds the integrations service. store/enc may be nil when the app runs
// without a database; in that case Enabled() returns false. pub is the
// provider-agnostic publisher for integration events; pass pubsub.Nop to disable.
func New(st *store.Store, enc crypto.Encryptor, cfg config.Config, resolve SessionResolver, pub pubsub.Publisher) *Service {
	if pub == nil {
		pub = pubsub.Nop
	}
	return &Service{store: st, enc: enc, cfg: cfg, resolve: resolve, pub: pub}
}

// Enabled reports whether persistence (and thus integrations) is available.
func (s *Service) Enabled() bool { return s.store != nil && s.enc != nil }

// ProviderStatus is the connection state of one provider for an org.
type ProviderStatus struct {
	Provider    string         `json:"provider"`
	Connected   bool           `json:"connected"`
	Account     string         `json:"account,omitempty"` // GitHub login / Slack team name
	ConnectedBy string         `json:"connectedBy,omitempty"`
	Metadata    map[string]any `json:"-"`
}

// Status returns the connection status for both providers for the given org.
func (s *Service) Status(ctx context.Context, orgID string) ([]ProviderStatus, error) {
	out := []ProviderStatus{
		{Provider: string(store.ProviderGitHub)},
		{Provider: string(store.ProviderSlack)},
		{Provider: string(store.ProviderLinear)},
	}
	if !s.Enabled() || orgID == "" {
		return out, nil
	}
	rows, err := s.store.ListIntegrations(ctx, orgID)
	if err != nil {
		return nil, err
	}
	byProvider := map[store.Provider]store.Integration{}
	for _, in := range rows {
		byProvider[in.Provider] = in
	}
	for i := range out {
		if in, ok := byProvider[store.Provider(out[i].Provider)]; ok {
			out[i].Connected = true
			out[i].ConnectedBy = in.ConnectedBy
			out[i].Account = accountLabel(in)
			out[i].Metadata = in.Metadata
		}
	}
	return out, nil
}

// Disconnect removes a provider integration for an org.
func (s *Service) Disconnect(ctx context.Context, orgID, provider string) error {
	if !s.Enabled() {
		return fmt.Errorf("integrations: not configured")
	}
	return s.store.DeleteIntegration(ctx, orgID, store.Provider(provider))
}

// redirectAfter builds the URL the browser returns to after a connect/callback,
// pointing at the SPA's integrations page with provider + status query flags so
// the UI can refresh and report the outcome. Defaults to the onboarding route;
// the SPA reuses the same flags on the settings page.
func (s *Service) redirectAfter(provider, status string) string {
	base := s.cfg.App.PostLoginURL
	return fmt.Sprintf("%s/onboarding?provider=%s&status=%s", base, provider, status)
}

// accountLabel derives a human label (GitHub login / Slack team name) from the
// stored metadata.
func accountLabel(in store.Integration) string {
	for _, k := range []string{"account_login", "team_name", "workspace_name", "login", "name"} {
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

type oauthState struct {
	OrgID  string `json:"org"`
	UserID string `json:"uid"`
	Nonce  string `json:"n"`
}

func (s *Service) sealState(st oauthState) (string, error) {
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
