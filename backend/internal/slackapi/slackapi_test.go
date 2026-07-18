package slackapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient spins up an httptest server whose handler is fn and returns a
// Client pointed at it, plus the server for cleanup.
func newTestClient(t *testing.T, fn http.HandlerFunc) Client {
	t.Helper()
	srv := httptest.NewServer(fn)
	t.Cleanup(srv.Close)
	return NewWithHTTP(srv.URL, srv.Client())
}

func TestPostMessage_SendsAttributionOverrides(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat.postMessage") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true,"ts":"1700000000.000100"}`))
	})

	ts, err := c.PostMessage(context.Background(), "xoxb-token", PostOptions{
		ChannelID: "C1",
		Text:      "hello",
		Username:  "Ada Lovelace",
		IconURL:   "https://example.com/ada.png",
		ThreadTS:  "1699999999.000000",
	})
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if ts != "1700000000.000100" {
		t.Errorf("ts = %q", ts)
	}
	if gotAuth != "Bearer xoxb-token" {
		t.Errorf("auth = %q", gotAuth)
	}
	// The attribution overrides must be forwarded verbatim.
	for k, want := range map[string]any{
		"channel":   "C1",
		"text":      "hello",
		"username":  "Ada Lovelace",
		"icon_url":  "https://example.com/ada.png",
		"thread_ts": "1699999999.000000",
	} {
		if gotBody[k] != want {
			t.Errorf("body[%q] = %v, want %v", k, gotBody[k], want)
		}
	}
}

func TestPostMessage_OmitsEmptyOverrides(t *testing.T) {
	var gotBody map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true,"ts":"1.2"}`))
	})
	if _, err := c.PostMessage(context.Background(), "t", PostOptions{ChannelID: "C1", Text: "hi"}); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	for _, k := range []string{"username", "icon_url", "thread_ts"} {
		if _, ok := gotBody[k]; ok {
			t.Errorf("expected %q to be omitted, got %v", k, gotBody[k])
		}
	}
}

func TestCreateChannel_ReturnsID(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"C999"}}`))
	})
	id, err := c.CreateChannel(context.Background(), "t", "tkt-sko-178")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if id != "C999" {
		t.Errorf("id = %q", id)
	}
}

func TestCall_SurfacesSlackError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"name_taken"}`))
	})
	_, err := c.CreateChannel(context.Background(), "t", "dup")
	if err == nil || !strings.Contains(err.Error(), "name_taken") {
		t.Fatalf("expected name_taken error, got %v", err)
	}
}

func TestInviteUsers_AlreadyInChannelIsBenign(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"already_in_channel"}`))
	})
	if err := c.InviteUsers(context.Background(), "t", "C1", []string{"UBOT"}); err != nil {
		t.Errorf("already_in_channel should be benign, got %v", err)
	}
}

func TestInviteUsers_EmptyIsNoop(t *testing.T) {
	called := false
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if err := c.InviteUsers(context.Background(), "t", "C1", nil); err != nil {
		t.Fatalf("InviteUsers: %v", err)
	}
	if called {
		t.Error("expected no HTTP call for empty user list")
	}
}

func TestListUsers_PaginatesAndFilters(t *testing.T) {
	// Page 1 returns a next_cursor; page 2 returns none. Includes a bot, a human,
	// and a deleted user (which must be dropped).
	page := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cursor := r.FormValue("cursor")
		if page == 0 {
			if cursor != "" {
				t.Errorf("first call should have no cursor, got %q", cursor)
			}
			page++
			_, _ = w.Write([]byte(`{"ok":true,"members":[
				{"id":"UBOT","name":"claude","is_bot":true,"profile":{"real_name":"Claude","image_192":"https://x/i.png"}},
				{"id":"UDEL","name":"gone","deleted":true}
			],"response_metadata":{"next_cursor":"CUR2"}}`))
			return
		}
		if cursor != "CUR2" {
			t.Errorf("second call cursor = %q, want CUR2", cursor)
		}
		_, _ = w.Write([]byte(`{"ok":true,"members":[
			{"id":"UHUMAN","name":"ada","is_bot":false,"profile":{"real_name":"Ada Lovelace"}}
		],"response_metadata":{"next_cursor":""}}`))
	})

	users, err := c.ListUsers(context.Background(), "t")
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("want 2 users (deleted dropped), got %d: %+v", len(users), users)
	}
	if users[0].ID != "UBOT" || !users[0].IsBot || users[0].Name != "Claude" {
		t.Errorf("bot user = %+v", users[0])
	}
	if users[1].ID != "UHUMAN" || users[1].IsBot || users[1].Name != "Ada Lovelace" {
		t.Errorf("human user = %+v", users[1])
	}
}

func TestLookupUserByEmail_MapsProfile(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"ada","profile":{"real_name":"Ada Lovelace","email":"ada@x.io","image_192":"https://x.io/a.png"}}}`))
	})
	u, err := c.LookupUserByEmail(context.Background(), "t", "ada@x.io")
	if err != nil {
		t.Fatalf("LookupUserByEmail: %v", err)
	}
	if u.ID != "U1" || u.Name != "Ada Lovelace" || u.IconURL != "https://x.io/a.png" {
		t.Errorf("user = %+v", u)
	}
}

func TestDownloadFile_SendsBearerAndCaps(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("filebytes"))
	}))
	t.Cleanup(srv.Close)
	c := NewWithHTTP(srv.URL, srv.Client())

	data, err := c.DownloadFile(context.Background(), "xoxb-token", srv.URL+"/files/f1")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if string(data) != "filebytes" {
		t.Errorf("data = %q", data)
	}
	// url_private only serves with the workspace token.
	if gotAuth != "Bearer xoxb-token" {
		t.Errorf("auth = %q", gotAuth)
	}
}

func TestDownloadFile_NonOKStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := NewWithHTTP(srv.URL, srv.Client())

	if _, err := c.DownloadFile(context.Background(), "t", srv.URL+"/gone"); err == nil {
		t.Fatal("want error on 404")
	}
}

// UploadFile is the three-step external upload: reserve a URL, send the bytes,
// complete against the channel/thread.
func TestUploadFile_ExternalUploadFlow(t *testing.T) {
	var gotBytes []byte
	var gotComplete map[string]any
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/files.getUploadURLExternal", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("filename") != "crash.png" || r.Form.Get("length") != "8" {
			t.Errorf("reserve params wrong: %v", r.Form)
		}
		_, _ = w.Write([]byte(`{"ok":true,"upload_url":"` + srv.URL + `/upload/v1/x","file_id":"F123"}`))
	})
	mux.HandleFunc("/upload/v1/x", func(w http.ResponseWriter, r *http.Request) {
		gotBytes, _ = io.ReadAll(r.Body)
	})
	mux.HandleFunc("/files.completeUploadExternal", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotComplete)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	c := NewWithHTTP(srv.URL, srv.Client())

	err := c.UploadFile(context.Background(), "xoxb-token", UploadOptions{
		ChannelID: "C1", ThreadTS: "TS1", Filename: "crash.png", Data: []byte("imgbytes"),
	})
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if string(gotBytes) != "imgbytes" {
		t.Errorf("uploaded bytes = %q", gotBytes)
	}
	if gotComplete["channel_id"] != "C1" || gotComplete["thread_ts"] != "TS1" {
		t.Errorf("complete params wrong: %v", gotComplete)
	}
	files, _ := gotComplete["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("complete files wrong: %v", gotComplete["files"])
	}
	f, _ := files[0].(map[string]any)
	if f["id"] != "F123" || f["title"] != "crash.png" {
		t.Errorf("complete file entry wrong: %v", f)
	}
}

func TestUploadFile_ReserveErrorSurfaces(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"missing_scope"}`))
	})
	err := c.UploadFile(context.Background(), "t", UploadOptions{ChannelID: "C1", Filename: "a", Data: []byte("x")})
	if err == nil || !strings.Contains(err.Error(), "missing_scope") {
		t.Fatalf("want missing_scope error, got %v", err)
	}
}

// Slack processes external uploads asynchronously: a slack_file block reference
// is rejected with invalid_blocks until processing completes. The client must
// retry that specific rejection.
func TestPostMessage_RetriesInvalidBlocksWhileFileProcesses(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_blocks"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"ts":"123.456"}`))
	})
	ts, err := c.PostMessage(context.Background(), "t", PostOptions{
		ChannelID: "C1", Text: "x",
		Blocks: []map[string]any{{"type": "image", "slack_file": map[string]any{"id": "F1"}, "alt_text": "x"}},
	})
	if err != nil || ts != "123.456" {
		t.Fatalf("want retry success, got ts=%q err=%v", ts, err)
	}
	if calls != 3 {
		t.Errorf("want 3 attempts, got %d", calls)
	}
}

// Without blocks, invalid_blocks-adjacent failures must NOT retry (nothing to
// wait for) — and other errors never retry even with blocks.
func TestPostMessage_NoBlocksNoRetry(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	})
	_, err := c.PostMessage(context.Background(), "t", PostOptions{
		ChannelID: "C1", Text: "x",
		Blocks: []map[string]any{{"type": "section"}},
	})
	if err == nil || calls != 1 {
		t.Fatalf("non-invalid_blocks error must not retry: calls=%d err=%v", calls, err)
	}
}
