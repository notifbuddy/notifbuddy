package pubsub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testAudience = "https://api.example.com/internal/pubsub/push"
	testSA       = "pusher@example.iam.gserviceaccount.com"
)

// fakeVerifier accepts the token "good" as testSA and "other" as a different
// principal; everything else is invalid.
type fakeVerifier struct{}

func (fakeVerifier) Verify(_ context.Context, token, audience string) (string, error) {
	if audience != testAudience {
		return "", fmt.Errorf("unexpected audience %q", audience)
	}
	switch token {
	case "good":
		return testSA, nil
	case "other":
		return "intruder@example.iam.gserviceaccount.com", nil
	default:
		return "", errors.New("bad token")
	}
}

func newTestPushServer(handle Handler) *pushServer {
	return &pushServer{
		subs: map[string]Subscription{
			"writer-linear": {
				Name:   "writer-linear",
				Topic:  "integrations.linear.webhook.received",
				Handle: handle,
			},
		},
		verify:    fakeVerifier{},
		audience:  testAudience,
		wantEmail: testSA,
	}
}

func pushBody(t *testing.T, subscription string, payload []byte, attrs map[string]string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"data":       base64.StdEncoding.EncodeToString(payload),
			"attributes": attrs,
			"messageId":  "m1",
		},
		"subscription":    subscription,
		"deliveryAttempt": 1,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return string(body)
}

func doPush(srv *pushServer, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", PushPath, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestPushServer_DispatchesToHandler(t *testing.T) {
	var got Message
	srv := newTestPushServer(func(_ context.Context, msg Message) error {
		got = msg
		return nil
	})

	body := pushBody(t, "projects/p/subscriptions/writer-linear",
		[]byte(`{"hello":"push"}`), map[string]string{"delivery_id": "d1"})
	rec := doPush(srv, "good", body)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
	}
	if got.Topic != "integrations.linear.webhook.received" {
		t.Errorf("topic = %q, want the subscription's topic", got.Topic)
	}
	if string(got.Payload) != `{"hello":"push"}` {
		t.Errorf("payload = %q (base64 decode failed?)", got.Payload)
	}
	if got.Attributes["delivery_id"] != "d1" {
		t.Errorf("attributes = %v, want delivery_id d1", got.Attributes)
	}
}

func TestPushServer_HandlerErrorNacks(t *testing.T) {
	srv := newTestPushServer(func(context.Context, Message) error {
		return errors.New("transient")
	})
	rec := doPush(srv, "good", pushBody(t, "projects/p/subscriptions/writer-linear", []byte("{}"), nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 so Pub/Sub redelivers", rec.Code)
	}
}

func TestPushServer_UnknownSubscriptionNacks(t *testing.T) {
	srv := newTestPushServer(func(context.Context, Message) error { return nil })
	rec := doPush(srv, "good", pushBody(t, "projects/p/subscriptions/nobody-home", []byte("{}"), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestPushServer_Auth(t *testing.T) {
	called := false
	srv := newTestPushServer(func(context.Context, Message) error {
		called = true
		return nil
	})
	body := pushBody(t, "projects/p/subscriptions/writer-linear", []byte("{}"), nil)

	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{"missing token", "", http.StatusUnauthorized},
		{"invalid token", "forged", http.StatusUnauthorized},
		{"wrong principal", "other", http.StatusForbidden},
	} {
		if rec := doPush(srv, tc.token, body); rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
	if called {
		t.Error("handler ran despite failed auth")
	}
}

func TestPushServer_MalformedBodyNacks(t *testing.T) {
	srv := newTestPushServer(func(context.Context, Message) error { return nil })
	rec := doPush(srv, "good", "not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}