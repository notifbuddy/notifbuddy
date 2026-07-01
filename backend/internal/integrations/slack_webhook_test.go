package integrations

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

// signSlack computes a valid X-Slack-Signature for body+timestamp, so the test
// exercises the real verification path rather than a hand-written constant.
func signSlack(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "v0:%s:%s", timestamp, body)
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestValidSlackSignature(t *testing.T) {
	secret := "shhh"
	body := []byte(`{"type":"event_callback"}`)
	now := time.Unix(1_700_000_000, 0)
	ts := "1700000000"
	good := signSlack(secret, ts, body)

	cases := []struct {
		name      string
		body      []byte
		timestamp string
		signature string
		want      bool
	}{
		{"valid", body, ts, good, true},
		{"tampered body", []byte(`{"type":"x"}`), ts, good, false},
		{"wrong secret", body, ts, signSlack("other", ts, body), false},
		{"stale timestamp", body, "1699000000", signSlack(secret, "1699000000", body), false},
		{"missing signature", body, ts, "", false},
		{"missing timestamp", body, "", good, false},
		{"non-numeric timestamp", body, "abc", good, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validSlackSignature(secret, tc.body, tc.timestamp, tc.signature, now)
			if got != tc.want {
				t.Errorf("validSlackSignature = %v, want %v", got, tc.want)
			}
		})
	}
}
