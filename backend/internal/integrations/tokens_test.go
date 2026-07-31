package integrations

import (
	"testing"
	"time"
)

func TestParseTokenBundle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want tokenBundle
	}{
		{
			name: "json bundle",
			in:   `{"access_token":"a","refresh_token":"r"}`,
			want: tokenBundle{AccessToken: "a", RefreshToken: "r"},
		},
		{
			name: "legacy plaintext",
			in:   "xoxb-legacy",
			want: tokenBundle{AccessToken: "xoxb-legacy"},
		},
		{
			name: "json missing access falls back to raw",
			in:   `{"refresh_token":"r"}`,
			want: tokenBundle{AccessToken: `{"refresh_token":"r"}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTokenBundle([]byte(tc.in))
			if got != tc.want {
				t.Fatalf("parseTokenBundle(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpiresWithin(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	skew := 2 * time.Minute

	past := now.Add(-time.Minute)
	soon := now.Add(time.Minute)
	later := now.Add(10 * time.Minute)

	if !expiresWithin(&past, skew, now) {
		t.Fatal("past expiry should refresh")
	}
	if !expiresWithin(&soon, skew, now) {
		t.Fatal("expiry within skew should refresh")
	}
	if expiresWithin(&later, skew, now) {
		t.Fatal("expiry beyond skew should not refresh")
	}
	if expiresWithin(nil, skew, now) {
		t.Fatal("nil expiry should not refresh")
	}
}

func TestExpiryFromExpiresIn(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	if got := expiryFromExpiresIn(0, now); got != nil {
		t.Fatalf("expires_in=0 → nil, got %v", got)
	}
	got := expiryFromExpiresIn(3600, now)
	if got == nil || !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("expires_in=3600 → %v, want %v", got, now.Add(time.Hour))
	}
}
