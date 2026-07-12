package integrations

import (
	"context"
	"testing"

	"xolo/backend/internal/config"
	"xolo/backend/internal/crypto"
	"xolo/backend/internal/store"
)

// newTestService builds a Service with a real encryptor but no store, so the
// no-DB paths (state sealing, Status shape) can be exercised without Postgres.
func newTestService(t *testing.T) *Service {
	t.Helper()
	enc, err := crypto.NewLocalKeyEncryptor(make([]byte, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	// store is nil => Enabled() is false; that's fine for these tests.
	return New(nil, enc, config.Config{}, nil, nil)
}

func TestOAuthState_RoundTripPreservesLevel(t *testing.T) {
	s := newTestService(t)
	for _, level := range []string{"", string(store.LevelWorkspace), string(store.LevelUser)} {
		in := oauthState{OrgID: "org_1", UserID: "user_1", Level: level, Nonce: "n"}
		sealed, err := s.sealState(in)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		got, err := s.openState(sealed)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if got.Level != level || got.OrgID != in.OrgID || got.UserID != in.UserID {
			t.Errorf("round-trip mismatch for level %q: got %+v", level, got)
		}
	}
}

func TestStatus_ShapeWhenNotEnabled(t *testing.T) {
	s := newTestService(t) // nil store => not enabled
	out, err := s.Status(context.Background(), "org_1", "user_1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	// Expect one entry per (provider, level): 2 providers x 2 levels.
	if len(out) != 4 {
		t.Fatalf("len(Status) = %d, want 4", len(out))
	}
	seen := map[string]bool{}
	for _, st := range out {
		if st.Connected {
			t.Errorf("expected all disconnected when not enabled, got connected %s/%s", st.Provider, st.Level)
		}
		if st.Level != string(store.LevelWorkspace) && st.Level != string(store.LevelUser) {
			t.Errorf("unexpected level %q", st.Level)
		}
		seen[st.Provider+"/"+st.Level] = true
	}
	for _, p := range []string{"slack", "linear"} {
		for _, l := range []string{"workspace", "user"} {
			if !seen[p+"/"+l] {
				t.Errorf("missing entry %s/%s", p, l)
			}
		}
	}
}
