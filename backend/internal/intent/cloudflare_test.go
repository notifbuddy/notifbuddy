package intent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xolo/backend/internal/config"
)

// newTestClassifier points a CloudflareClassifier at a test server URL by
// overriding the base via a custom transport that rewrites the host. Simpler:
// we construct the classifier with real-looking creds and swap its http.Client
// to one whose transport redirects to the test server.
func newTestClassifier(t *testing.T, srv *httptest.Server) *CloudflareClassifier {
	t.Helper()
	c := NewCloudflareClassifier(config.CloudflareConfig{
		AccountID: "acct123",
		APIToken:  "token-abc",
		Model:     "@cf/test/model",
	})
	// Route every request to the test server regardless of the real CF host.
	c.http = srv.Client()
	c.http.Transport = rewriteTransport{base: srv.URL, rt: srv.Client().Transport}
	return c
}

// rewriteTransport sends all requests to base while preserving the original
// path, so we can still assert the account/model path the classifier built.
type rewriteTransport struct {
	base string
	rt   http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, _ := http.NewRequest(req.Method, t.base+req.URL.Path, req.Body)
	target.Header = req.Header
	target = target.WithContext(req.Context())
	rt := t.rt
	if rt == nil {
		rt = http.DefaultTransport
	}
	return rt.RoundTrip(target)
}

// okResponse builds a successful Workers AI envelope wrapping the given model
// text, as a raw JSON map so tests don't depend on cfRunResponse's anonymous
// struct shape.
func okResponse(modelResponse string) map[string]any {
	return map[string]any{
		"success": true,
		"errors":  []any{},
		"result":  map[string]string{"response": modelResponse},
	}
}

// cfHandler returns a handler that replies with the given model response text,
// optionally capturing the decoded request.
func cfHandler(t *testing.T, modelResponse string, captured *cfRunRequest) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, captured)
		}
		_ = json.NewEncoder(w).Encode(okResponse(modelResponse))
	}
}

func TestClassify_MapsModelResponseToIntent(t *testing.T) {
	cases := []struct {
		name          string
		modelResponse string
		want          Intent
	}{
		{"json create", `{"action":"create-channel"}`, CreateChannel},
		{"json close", `{"action":"close-channel"}`, CloseChannel},
		{"json no-action", `{"action":"no-action"}`, NoAction},
		{"json with prose", "Sure! {\"action\":\"create-channel\"}", CreateChannel},
		{"json in code fence", "```json\n{\"action\":\"close-channel\"}\n```", CloseChannel},
		{"bare command", "create-channel", CreateChannel},
		{"unknown action", `{"action":"delete-everything"}`, NoAction},
		{"garbage", "I am a helpful assistant.", NoAction},
		{"malformed json", `{"action":`, NoAction},
		{"empty response", "", NoAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(cfHandler(t, tc.modelResponse, nil))
			defer srv.Close()
			c := newTestClassifier(t, srv)

			got := c.Classify(context.Background(), "@notifbuddy do the thing")
			if got != tc.want {
				t.Fatalf("Classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestClassify_HandlesResponseShapes covers the three envelope shapes Workers AI
// returns depending on the model: response-as-string (small models),
// response-as-object (chat models in JSON mode, e.g. llama-3.1-8b), and
// OpenAI-style choices[].message.content. A regression here previously made the
// 8B model decode-fail and fall through to no-action.
func TestClassify_HandlesResponseShapes(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want Intent
	}{
		{
			name: "response as string",
			body: map[string]any{
				"success": true, "errors": []any{},
				"result": map[string]any{"response": `{"action":"create-channel"}`},
			},
			want: CreateChannel,
		},
		{
			name: "response as object",
			body: map[string]any{
				"success": true, "errors": []any{},
				"result": map[string]any{"response": map[string]string{"action": "close-channel"}},
			},
			want: CloseChannel,
		},
		{
			name: "openai-style choices content",
			body: map[string]any{
				"success": true, "errors": []any{},
				"result": map[string]any{
					"choices": []map[string]any{
						{"message": map[string]string{"content": `{"action":"create-channel"}`}},
					},
				},
			},
			want: CreateChannel,
		},
		{
			name: "object response with both fields present (object wins)",
			body: map[string]any{
				"success": true, "errors": []any{},
				"result": map[string]any{
					"response": map[string]string{"action": "close-channel"},
					"choices": []map[string]any{
						{"message": map[string]string{"content": `{"action":"create-channel"}`}},
					},
				},
			},
			want: CloseChannel,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(tc.body)
			}))
			defer srv.Close()
			c := newTestClassifier(t, srv)

			if got := c.Classify(context.Background(), "do the thing"); got != tc.want {
				t.Fatalf("Classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassify_BuildsCorrectRequest(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var captured cfRunRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(okResponse(`{"action":"create-channel"}`))
	}))
	defer srv.Close()
	c := newTestClassifier(t, srv)

	const userText = "please slack this PR"
	c.Classify(context.Background(), userText)

	if want := "/client/v4/accounts/acct123/ai/run/@cf/test/model"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if want := "Bearer token-abc"; gotAuth != want {
		t.Errorf("auth = %q, want %q", gotAuth, want)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotContentType)
	}
	if len(captured.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(captured.Messages))
	}
	if captured.Messages[0].Role != "system" || captured.Messages[1].Role != "user" {
		t.Errorf("roles = %q/%q, want system/user", captured.Messages[0].Role, captured.Messages[1].Role)
	}
	if captured.Messages[1].Content != userText {
		t.Errorf("user content = %q, want %q (raw text, untouched)", captured.Messages[1].Content, userText)
	}
	if !strings.Contains(captured.Messages[0].Content, "create-channel") {
		t.Errorf("system prompt missing command catalog")
	}
}

func TestClassify_DefaultsModel(t *testing.T) {
	c := NewCloudflareClassifier(config.CloudflareConfig{AccountID: "a", APIToken: "t"})
	if c.model != DefaultCloudflareModel {
		t.Fatalf("model = %q, want default %q", c.model, DefaultCloudflareModel)
	}
}

func TestClassify_UnconfiguredReturnsNoActionWithoutHTTP(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer srv.Close()

	// Missing API token => unconfigured.
	c := NewCloudflareClassifier(config.CloudflareConfig{AccountID: "a"})
	c.http = srv.Client()
	c.http.Transport = rewriteTransport{base: srv.URL}

	if got := c.Classify(context.Background(), "create slack channel"); got != NoAction {
		t.Fatalf("Classify() = %q, want no-action", got)
	}
	if called {
		t.Fatalf("unconfigured classifier made an HTTP call")
	}
}

func TestClassify_EmptyTextReturnsNoActionWithoutHTTP(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer srv.Close()
	c := newTestClassifier(t, srv)

	if got := c.Classify(context.Background(), "   "); got != NoAction {
		t.Fatalf("Classify() = %q, want no-action", got)
	}
	if called {
		t.Fatalf("blank text triggered an HTTP call")
	}
}

func TestClassify_Non200ReturnsNoAction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := newTestClassifier(t, srv)

	if got := c.Classify(context.Background(), "create slack channel"); got != NoAction {
		t.Fatalf("Classify() = %q, want no-action on non-200", got)
	}
}

func TestClassify_SuccessFalseReturnsNoAction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"errors":  []map[string]string{{"message": "model unavailable"}},
			"result":  map[string]string{"response": `{"action":"create-channel"}`},
		})
	}))
	defer srv.Close()
	c := newTestClassifier(t, srv)

	if got := c.Classify(context.Background(), "create slack channel"); got != NoAction {
		t.Fatalf("Classify() = %q, want no-action when success=false", got)
	}
}

func TestClassify_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(okResponse(`{"action":"create-channel"}`))
	}))
	defer srv.Close()
	c := newTestClassifier(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if got := c.Classify(ctx, "create slack channel"); got != NoAction {
		t.Fatalf("Classify() = %q, want no-action on canceled context", got)
	}
}
