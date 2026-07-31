package integrations

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"xolo/backend/internal/crypto"
	"xolo/backend/internal/lock"
	"xolo/backend/internal/store"
)

const oauthRefreshSkew = 2 * time.Minute

type tokenRefreshFunc func(ctx context.Context, refreshToken string) (access, refresh string, expiresIn int, err error)

type oauthTokenStore interface {
	GetIntegration(ctx context.Context, orgID string, provider store.Provider, level store.Level, userID string) (*store.Integration, error)
	UpdateIntegrationTokens(ctx context.Context, orgID string, provider store.Provider, level store.Level, userID string, encryptedToken []byte, expiresAt *time.Time) error
}

type oauthRoundTripper struct {
	base       http.RoundTripper
	locker     lock.Locker
	store      oauthTokenStore
	enc        crypto.Encryptor
	orgID      string
	provider   store.Provider
	level      store.Level
	userID     string
	refresh    tokenRefreshFunc
	formatAuth func(accessToken string) string
	now        func() time.Time
}

func (t *oauthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.base == nil {
		t.base = http.DefaultTransport
	}
	if t.now == nil {
		t.now = time.Now
	}
	access, err := t.ensureAccess(req.Context())
	if err != nil {
		return nil, err
	}
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", t.formatAuth(access))
	return t.base.RoundTrip(req2)
}

func (t *oauthRoundTripper) ensureAccess(ctx context.Context) (string, error) {
	in, err := t.store.GetIntegration(ctx, t.orgID, t.provider, t.level, t.userID)
	if err != nil {
		return "", err
	}
	plain, err := t.enc.Decrypt(in.EncryptedToken)
	if err != nil {
		return "", fmt.Errorf("integrations: decrypt token: %w", err)
	}
	bundle := parseTokenBundle(plain)

	if !expiresWithin(in.TokenExpiresAt, oauthRefreshSkew, t.now()) || bundle.RefreshToken == "" || t.refresh == nil {
		if bundle.AccessToken == "" {
			return "", fmt.Errorf("integrations: empty access token")
		}
		return bundle.AccessToken, nil
	}

	key := oauthLockKey(t.orgID, t.provider, t.level, t.userID)
	var access string
	err = t.locker.WithLock(ctx, key, func(ctx context.Context) error {
		in, err := t.store.GetIntegration(ctx, t.orgID, t.provider, t.level, t.userID)
		if err != nil {
			return err
		}
		plain, err := t.enc.Decrypt(in.EncryptedToken)
		if err != nil {
			return fmt.Errorf("integrations: decrypt token: %w", err)
		}
		bundle := parseTokenBundle(plain)
		if !expiresWithin(in.TokenExpiresAt, oauthRefreshSkew, t.now()) || bundle.RefreshToken == "" {
			access = bundle.AccessToken
			return nil
		}

		newAccess, newRefresh, expiresIn, err := t.refresh(ctx, bundle.RefreshToken)
		if err != nil {
			return fmt.Errorf("integrations: refresh %s token: %w", t.provider, err)
		}
		if newAccess == "" {
			return fmt.Errorf("integrations: refresh %s token: empty access token", t.provider)
		}
		if newRefresh == "" {
			newRefresh = bundle.RefreshToken
		}
		raw, err := marshalTokenBundle(tokenBundle{
			AccessToken:  newAccess,
			RefreshToken: newRefresh,
		})
		if err != nil {
			return err
		}
		enc, err := t.enc.Encrypt(raw)
		if err != nil {
			return err
		}
		exp := expiryFromExpiresIn(expiresIn, t.now())
		if err := t.store.UpdateIntegrationTokens(ctx, t.orgID, t.provider, t.level, t.userID, enc, exp); err != nil {
			return err
		}
		access = newAccess
		return nil
	})
	if err != nil {
		return "", err
	}
	if access == "" {
		return "", fmt.Errorf("integrations: empty access token after refresh")
	}
	return access, nil
}

func oauthLockKey(orgID string, provider store.Provider, level store.Level, userID string) string {
	return fmt.Sprintf("oauth-refresh:%s:%s:%s:%s", orgID, provider, level.Norm(), userID)
}

func (s *Service) oauthHTTPClient(orgID string, provider store.Provider, level store.Level, userID string, refresh tokenRefreshFunc, formatAuth func(string) string) *http.Client {
	if formatAuth == nil {
		formatAuth = func(tok string) string { return tok }
	}
	locker := s.locker
	if locker == nil {
		locker = lock.Nop{}
	}
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &oauthRoundTripper{
			base:       http.DefaultTransport,
			locker:     locker,
			store:      s.store,
			enc:        s.enc,
			orgID:      orgID,
			provider:   provider,
			level:      level.Norm(),
			userID:     userID,
			refresh:    refresh,
			formatAuth: formatAuth,
		},
	}
}
