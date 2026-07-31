package integrations

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xolo/backend/internal/crypto"
	"xolo/backend/internal/lock"
	"xolo/backend/internal/store"
)

type testLocker struct {
	mu sync.Mutex
}

func (t *testLocker) WithLock(ctx context.Context, _ string, fn func(context.Context) error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return fn(ctx)
}

type memTokenStore struct {
	mu  sync.Mutex
	row *store.Integration
}

func (m *memTokenStore) GetIntegration(_ context.Context, orgID string, provider store.Provider, level store.Level, userID string) (*store.Integration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.row == nil {
		return nil, store.ErrNotFound
	}
	cp := *m.row
	cp.OrgID, cp.Provider, cp.Level, cp.ConnectedUserID = orgID, provider, level.Norm(), userID
	if len(m.row.EncryptedToken) > 0 {
		cp.EncryptedToken = append([]byte(nil), m.row.EncryptedToken...)
	}
	if m.row.TokenExpiresAt != nil {
		t := *m.row.TokenExpiresAt
		cp.TokenExpiresAt = &t
	}
	return &cp, nil
}

func (m *memTokenStore) UpdateIntegrationTokens(_ context.Context, _ string, _ store.Provider, _ store.Level, _ string, encryptedToken []byte, expiresAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.row == nil {
		return store.ErrNotFound
	}
	m.row.EncryptedToken = append([]byte(nil), encryptedToken...)
	if expiresAt != nil {
		t := *expiresAt
		m.row.TokenExpiresAt = &t
	} else {
		m.row.TokenExpiresAt = nil
	}
	return nil
}

func testEnc(t *testing.T) crypto.Encryptor {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	enc, err := crypto.NewLocalKeyEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

func sealBundle(t *testing.T, enc crypto.Encryptor, access, refresh string) []byte {
	t.Helper()
	raw, err := marshalTokenBundle(tokenBundle{AccessToken: access, RefreshToken: refresh})
	if err != nil {
		t.Fatal(err)
	}
	ct, err := enc.Encrypt(raw)
	if err != nil {
		t.Fatal(err)
	}
	return ct
}

func TestOAuthRoundTripper_NoRefreshWhenFarFromExpiry(t *testing.T) {
	t.Parallel()
	enc := testEnc(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	exp := now.Add(10 * time.Hour)
	st := &memTokenStore{row: &store.Integration{
		EncryptedToken: sealBundle(t, enc, "access-1", "refresh-1"),
		TokenExpiresAt: &exp,
	}}
	var refreshes atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "access-1" {
			t.Errorf("Authorization = %q, want access-1", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer api.Close()

	rt := &oauthRoundTripper{
		base:       api.Client().Transport,
		locker:     lock.Nop{},
		store:      st,
		enc:        enc,
		orgID:      "org",
		provider:   store.ProviderLinear,
		level:      store.LevelWorkspace,
		refresh:    func(context.Context, string) (string, string, int, error) { refreshes.Add(1); return "", "", 0, nil },
		formatAuth: func(tok string) string { return tok },
		now:        func() time.Time { return now },
	}
	req, _ := http.NewRequest(http.MethodGet, api.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if refreshes.Load() != 0 {
		t.Fatalf("refresh called %d times, want 0", refreshes.Load())
	}
}

func TestOAuthRoundTripper_RefreshesWhenWithinSkew(t *testing.T) {
	t.Parallel()
	enc := testEnc(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	exp := now.Add(time.Minute) // within 2m skew
	st := &memTokenStore{row: &store.Integration{
		EncryptedToken: sealBundle(t, enc, "access-old", "refresh-old"),
		TokenExpiresAt: &exp,
	}}
	var refreshes atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "access-new" {
			t.Errorf("Authorization = %q, want access-new", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()

	rt := &oauthRoundTripper{
		base:     api.Client().Transport,
		locker:   lock.Nop{},
		store:    st,
		enc:      enc,
		orgID:    "org",
		provider: store.ProviderLinear,
		level:    store.LevelWorkspace,
		refresh: func(_ context.Context, refresh string) (string, string, int, error) {
			refreshes.Add(1)
			if refresh != "refresh-old" {
				t.Errorf("refresh token = %q", refresh)
			}
			return "access-new", "refresh-new", 86400, nil
		},
		formatAuth: func(tok string) string { return tok },
		now:        func() time.Time { return now },
	}
	req, _ := http.NewRequest(http.MethodGet, api.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if refreshes.Load() != 1 {
		t.Fatalf("refresh called %d times, want 1", refreshes.Load())
	}

	in, err := st.GetIntegration(context.Background(), "org", store.ProviderLinear, store.LevelWorkspace, "")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := enc.Decrypt(in.EncryptedToken)
	if err != nil {
		t.Fatal(err)
	}
	b := parseTokenBundle(plain)
	if b.AccessToken != "access-new" || b.RefreshToken != "refresh-new" {
		t.Fatalf("persisted bundle = %+v", b)
	}
	if in.TokenExpiresAt == nil || !in.TokenExpiresAt.Equal(now.Add(86400*time.Second)) {
		t.Fatalf("expires_at = %v", in.TokenExpiresAt)
	}
}

func TestOAuthRoundTripper_ConcurrentRefreshSingleFlight(t *testing.T) {
	t.Parallel()
	enc := testEnc(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	exp := now.Add(30 * time.Second)
	st := &memTokenStore{row: &store.Integration{
		EncryptedToken: sealBundle(t, enc, "access-old", "refresh-old"),
		TokenExpiresAt: &exp,
	}}
	var refreshes atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer api.Close()

	rt := &oauthRoundTripper{
		base:     api.Client().Transport,
		locker:   &testLocker{},
		store:    st,
		enc:      enc,
		orgID:    "org",
		provider: store.ProviderLinear,
		level:    store.LevelWorkspace,
		refresh: func(context.Context, string) (string, string, int, error) {
			n := refreshes.Add(1)
			if n == 1 {
				close(started)
				<-release
			}
			return "access-new", "refresh-new", 86400, nil
		},
		formatAuth: func(tok string) string { return tok },
		now:        func() time.Time { return now },
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, api.URL, nil)
			resp, err := rt.RoundTrip(req)
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refresh called %d times, want 1 (lock single-flight)", refreshes.Load())
	}
}

func TestMarshalTokenBundleRoundTrip(t *testing.T) {
	t.Parallel()
	raw, err := marshalTokenBundle(tokenBundle{AccessToken: "a", RefreshToken: "r"})
	if err != nil {
		t.Fatal(err)
	}
	var got tokenBundle
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "a" || got.RefreshToken != "r" {
		t.Fatalf("got %+v", got)
	}
}
