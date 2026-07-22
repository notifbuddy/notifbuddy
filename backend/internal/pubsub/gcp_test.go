package pubsub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGCPBus_resourceName(t *testing.T) {
	for _, tc := range []struct {
		prefix, logical, want string
	}{
		{"", "writer-linear", "writer-linear"},
		{"", "integrations.linear.webhook.received", "integrations.linear.webhook.received"},
		{"pr-57-", "writer-linear", "pr-57-writer-linear"},
		{"pr-57-", "integrations.linear.webhook.received", "pr-57-integrations.linear.webhook.received"},
		{"pr-1-", "pubsub.poison", "pr-1-pubsub.poison"},
	} {
		b := &GCPBus{opts: GCPOptions{NamePrefix: tc.prefix}}
		if got := b.resourceName(tc.logical); got != tc.want {
			t.Errorf("prefix %q + %q = %q, want %q", tc.prefix, tc.logical, got, tc.want)
		}
	}
}

func TestGCPBus_StartKeysPushByPrefixedName(t *testing.T) {
	b := &GCPBus{
		opts: GCPOptions{
			NamePrefix:         "pr-57-",
			PushAudience:       testAudience,
			PushServiceAccount: testSA,
			Verifier:           fakeVerifier{},
		},
	}
	var got Message
	if err := b.Start(t.Context(), []Subscription{{
		Name:  "writer-linear",
		Topic: "integrations.linear.webhook.received",
		Handle: func(_ context.Context, msg Message) error {
			got = msg
			return nil
		},
	}}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	h := b.PushHandler()
	if h == nil {
		t.Fatal("PushHandler nil after Start")
	}
	body := pushBody(t, "projects/p/subscriptions/pr-57-writer-linear",
		[]byte(`{"ok":true}`), nil)
	req := httptest.NewRequest("POST", PushPath, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
	}
	if got.Topic != "integrations.linear.webhook.received" {
		t.Errorf("handler topic = %q, want logical manifest topic", got.Topic)
	}

	// Logical (unprefixed) subscription name must not match — CI creates the
	// prefixed GCP resource, so a bare name is a misconfiguration.
	rec = httptest.NewRecorder()
	body = pushBody(t, "projects/p/subscriptions/writer-linear", []byte(`{}`), nil)
	req = httptest.NewRequest("POST", PushPath, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer good")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unprefixed sub status = %d, want 404", rec.Code)
	}
}
