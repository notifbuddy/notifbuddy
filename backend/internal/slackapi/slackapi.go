// Package slackapi is a thin client for the Slack Web API methods the sync
// engine needs: creating/archiving/deleting channels, inviting users (auto-add
// bots), posting messages with per-message author overrides, and resolving a
// user by email.
//
// It follows the same seam style as internal/pubsub and internal/crypto:
// callers depend only on the Client interface; the concrete HTTP implementation
// never leaks its transport types. The Slack token (a workspace bot token,
// xoxb) is passed per call rather than held on the client, because the caller
// (integrations.Service) owns token storage/decryption and the same client
// serves many orgs.
package slackapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PostOptions carries the per-message overrides for chat.postMessage. Username
// and IconURL drive the native "app posting on behalf of a user" attribution
// (requires the chat:write.customize scope): the message is authored by the bot
// but displays the given name/avatar. ThreadTS, when set, posts the message as
// a reply in that thread.
type PostOptions struct {
	ChannelID string
	Text      string
	Username  string // display name override (attribution); empty = the bot's own name
	IconURL   string // display avatar override (attribution); empty = the bot's own icon
	ThreadTS  string // parent message ts to reply under; empty = a top-level message
	// Blocks, when set, renders instead of Text (which becomes the notification
	// fallback). Used to compose text + inline images as one message.
	Blocks []map[string]any
}

// UpdateOptions edits an existing bot-authored message in place (chat.update).
// Used to graft late-arriving attachments onto the already-mirrored message so
// they stay one entity instead of a separate post.
type UpdateOptions struct {
	ChannelID string
	TS        string
	Text      string
	Blocks    []map[string]any
}

// UploadOptions describes one file to share into a channel via Slack's
// external upload flow (files.getUploadURLExternal + files.completeUploadExternal).
// ThreadTS, when set, shares the file as a reply in that thread.
type UploadOptions struct {
	ChannelID string
	ThreadTS  string
	Filename  string
	Data      []byte
}

// MiB is one mebibyte, for readable size constants.
const MiB = 1 << 20

// MaxFileBytes caps file transfers in both directions. Files land fully in
// memory during a mirror, so the cap protects the process, not the providers
// (both Slack and Linear allow far larger uploads).
const MaxFileBytes = 25 * MiB

// User is the subset of a Slack user we use for attribution/routing.
type User struct {
	ID      string
	Name    string // display or real name
	Email   string
	IconURL string // profile image (image_192)
	IsBot   bool
}

// Client is the Slack Web API surface the sync engine depends on. All methods
// take the workspace bot token as their first non-context argument.
// Implementations must be safe for concurrent use.
type Client interface {
	// CreateChannel creates a public channel with the given (already-sanitized)
	// name and returns its channel id.
	CreateChannel(ctx context.Context, token, name string) (channelID string, err error)
	// ArchiveChannel archives (closes) a channel. Archiving is Slack's notion of
	// "closing" a channel; it is reversible.
	ArchiveChannel(ctx context.Context, token, channelID string) error
	// DeleteChannel permanently deletes a channel. Slack only exposes this to
	// admin/org tokens; most callers should prefer ArchiveChannel.
	DeleteChannel(ctx context.Context, token, channelID string) error
	// InviteUsers adds the given user ids to a channel (used to auto-add bots on
	// creation). Already-in-channel ids are not treated as an error.
	InviteUsers(ctx context.Context, token, channelID string, userIDs []string) error
	// PostMessage posts a message and returns its ts (the message id used as a
	// thread anchor).
	PostMessage(ctx context.Context, token string, opts PostOptions) (ts string, err error)
	// LookupUserByEmail resolves a workspace user by email (used to map a Linear
	// comment author to their Slack identity for attribution/mentions).
	LookupUserByEmail(ctx context.Context, token, email string) (User, error)
	// UserByID resolves a workspace user by id (used to attribute a Slack
	// message's author on the mirrored Linear comment).
	UserByID(ctx context.Context, token, userID string) (User, error)
	// AuthTestUserID returns the bot user id for the token (users.identity via
	// auth.test), used by the sync engine's loop guard to drop the bot's own
	// message events.
	AuthTestUserID(ctx context.Context, token string) (string, error)
	// ListUsers returns every member of the workspace (bots and humans), used to
	// populate the auto-add pickers. It walks Slack's cursor pagination.
	ListUsers(ctx context.Context, token string) ([]User, error)
	// DownloadFile fetches a Slack file's bytes from its url_private (requires
	// the files:read scope; the URL only serves with the workspace token).
	DownloadFile(ctx context.Context, token, fileURL string) ([]byte, error)
	// UploadFile shares a file into a channel (requires files:write). It is
	// Slack's two-step external upload: reserve an upload URL, send the bytes,
	// then complete the upload against the channel/thread.
	UploadFile(ctx context.Context, token string, opts UploadOptions) error
	// UpdateMessage edits a bot-authored message's text/blocks in place.
	UpdateMessage(ctx context.Context, token string, opts UpdateOptions) error
}

// httpClient is the default Client, talking to https://slack.com/api. It is
// stateless apart from the underlying *http.Client.
type httpClient struct {
	hc      *http.Client
	baseURL string
	// blockRetryDelay paces retries of block payloads rejected with
	// invalid_blocks — Slack processes uploads asynchronously, and a slack_file
	// reference is invalid until processing completes (typically a few seconds).
	blockRetryDelay time.Duration
}

// New returns the default HTTP-backed Slack client.
func New() Client {
	return &httpClient{hc: http.DefaultClient, baseURL: "https://slack.com/api", blockRetryDelay: 2 * time.Second}
}

// NewWithHTTP returns a client with a custom base URL and *http.Client, for
// tests (point baseURL at an httptest server).
func NewWithHTTP(baseURL string, hc *http.Client) Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &httpClient{hc: hc, baseURL: strings.TrimRight(baseURL, "/"), blockRetryDelay: 10 * time.Millisecond}
}

func (c *httpClient) CreateChannel(ctx context.Context, token, name string) (string, error) {
	var out struct {
		slackOK
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	if err := c.callJSON(ctx, token, "conversations.create", map[string]any{"name": name}, &out); err != nil {
		return "", err
	}
	return out.Channel.ID, nil
}

func (c *httpClient) ArchiveChannel(ctx context.Context, token, channelID string) error {
	var out slackOK
	return c.callJSON(ctx, token, "conversations.archive", map[string]any{"channel": channelID}, &out)
}

func (c *httpClient) DeleteChannel(ctx context.Context, token, channelID string) error {
	var out slackOK
	return c.callJSON(ctx, token, "conversations.delete", map[string]any{"channel": channelID}, &out)
}

func (c *httpClient) InviteUsers(ctx context.Context, token, channelID string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	var out slackOK
	err := c.callJSON(ctx, token, "conversations.invite", map[string]any{
		"channel": channelID,
		"users":   strings.Join(userIDs, ","),
	}, &out)
	// already_in_channel is a benign outcome for an idempotent auto-add.
	if err != nil && strings.Contains(err.Error(), "already_in_channel") {
		return nil
	}
	return err
}

func (c *httpClient) PostMessage(ctx context.Context, token string, opts PostOptions) (string, error) {
	body := map[string]any{
		"channel": opts.ChannelID,
		"text":    opts.Text,
	}
	if opts.Username != "" {
		body["username"] = opts.Username
	}
	if opts.IconURL != "" {
		body["icon_url"] = opts.IconURL
	}
	if opts.ThreadTS != "" {
		body["thread_ts"] = opts.ThreadTS
	}
	if len(opts.Blocks) > 0 {
		body["blocks"] = opts.Blocks
	}
	var out struct {
		slackOK
		TS string `json:"ts"`
	}
	if err := c.callBlocksRetry(ctx, token, "chat.postMessage", body, &out, len(opts.Blocks) > 0); err != nil {
		return "", err
	}
	return out.TS, nil
}

// callBlocksRetry calls a message method, retrying invalid_blocks when the
// payload carries blocks: a slack_file reference is rejected with exactly that
// error until Slack finishes processing the upload (async, a few seconds).
// invalid_blocks means nothing was posted, so the retry cannot double-post.
func (c *httpClient) callBlocksRetry(ctx context.Context, token, method string, body map[string]any, out okResponse, hasBlocks bool) error {
	const attempts = 4
	var err error
	for i := range attempts {
		if i > 0 {
			select {
			case <-time.After(c.blockRetryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err = c.callJSON(ctx, token, method, body, out)
		if err == nil || !hasBlocks || !strings.Contains(err.Error(), "invalid_blocks") {
			return err
		}
	}
	return err
}

func (c *httpClient) LookupUserByEmail(ctx context.Context, token, email string) (User, error) {
	form := url.Values{}
	form.Set("email", email)
	var out struct {
		slackOK
		User slackUser `json:"user"`
	}
	if err := c.callForm(ctx, token, "users.lookupByEmail", form, &out); err != nil {
		return User{}, err
	}
	return out.User.toUser(), nil
}

func (c *httpClient) UserByID(ctx context.Context, token, userID string) (User, error) {
	form := url.Values{}
	form.Set("user", userID)
	var out struct {
		slackOK
		User slackUser `json:"user"`
	}
	if err := c.callForm(ctx, token, "users.info", form, &out); err != nil {
		return User{}, err
	}
	return out.User.toUser(), nil
}

func (c *httpClient) AuthTestUserID(ctx context.Context, token string) (string, error) {
	var out struct {
		slackOK
		UserID string `json:"user_id"`
	}
	if err := c.callForm(ctx, token, "auth.test", url.Values{}, &out); err != nil {
		return "", err
	}
	return out.UserID, nil
}

func (c *httpClient) ListUsers(ctx context.Context, token string) ([]User, error) {
	// users.list is cursor-paginated (Slack recommends <=200/page). Walk every
	// page via response_metadata.next_cursor until it comes back empty. Cap the
	// page count as a runaway guard for very large workspaces.
	var users []User
	cursor := ""
	for range 100 {
		form := url.Values{}
		form.Set("limit", "200")
		if cursor != "" {
			form.Set("cursor", cursor)
		}
		var out struct {
			slackOK
			Members  []slackUser `json:"members"`
			Metadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.callForm(ctx, token, "users.list", form, &out); err != nil {
			return nil, err
		}
		for _, m := range out.Members {
			if m.Deleted {
				continue // deactivated users can't be invited; don't offer them
			}
			users = append(users, m.toUser())
		}
		cursor = out.Metadata.NextCursor
		if cursor == "" {
			break
		}
	}
	return users, nil
}

func (c *httpClient) DownloadFile(ctx context.Context, token, fileURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slackapi: download file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slackapi: download file: status %d", resp.StatusCode)
	}
	// Read one byte past the cap so an oversized file is detected rather than
	// silently truncated into a corrupt mirror.
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("slackapi: download file: read: %w", err)
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("slackapi: download file: exceeds %d byte cap", MaxFileBytes)
	}
	return data, nil
}

// pushFileBytes reserves an external-upload URL for the file and sends the
// bytes, returning the pending file id (not yet completed/shared).
func (c *httpClient) pushFileBytes(ctx context.Context, token, filename string, data []byte) (string, error) {
	form := url.Values{}
	form.Set("filename", filename)
	form.Set("length", fmt.Sprint(len(data)))
	var reserved struct {
		slackOK
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	if err := c.callForm(ctx, token, "files.getUploadURLExternal", form, &reserved); err != nil {
		return "", err
	}

	// The upload_url is pre-signed — no Authorization header (matching Slack's
	// official SDKs; an extra header can be rejected by the storage backend).
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reserved.UploadURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("slackapi: upload file bytes: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("slackapi: upload file bytes: status %d", resp.StatusCode)
	}
	return reserved.FileID, nil
}

func (c *httpClient) completeUpload(ctx context.Context, token, fileID, title, channelID, threadTS string) error {
	body := map[string]any{
		"files": []map[string]any{{"id": fileID, "title": title}},
	}
	if channelID != "" {
		body["channel_id"] = channelID
	}
	if threadTS != "" {
		body["thread_ts"] = threadTS
	}
	var done slackOK
	return c.callJSON(ctx, token, "files.completeUploadExternal", body, &done)
}

func (c *httpClient) UploadFile(ctx context.Context, token string, opts UploadOptions) error {
	fileID, err := c.pushFileBytes(ctx, token, opts.Filename, opts.Data)
	if err != nil {
		return err
	}
	return c.completeUpload(ctx, token, fileID, opts.Filename, opts.ChannelID, opts.ThreadTS)
}

func (c *httpClient) UpdateMessage(ctx context.Context, token string, opts UpdateOptions) error {
	body := map[string]any{
		"channel": opts.ChannelID,
		"ts":      opts.TS,
		"text":    opts.Text,
	}
	if len(opts.Blocks) > 0 {
		body["blocks"] = opts.Blocks
	}
	var out slackOK
	return c.callBlocksRetry(ctx, token, "chat.update", body, &out, len(opts.Blocks) > 0)
}

// slackOK is embedded in every response to surface the Web API's uniform
// {ok, error} envelope.
type slackOK struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type slackUser struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsBot   bool   `json:"is_bot"`
	Deleted bool   `json:"deleted"`
	Profile struct {
		RealName string `json:"real_name"`
		Email    string `json:"email"`
		Image192 string `json:"image_192"`
	} `json:"profile"`
}

func (u slackUser) toUser() User {
	name := u.Profile.RealName
	if name == "" {
		name = u.Name
	}
	return User{ID: u.ID, Name: name, Email: u.Profile.Email, IconURL: u.Profile.Image192, IsBot: u.IsBot}
}

// callJSON POSTs a JSON body to a Web API method with a Bearer token and decodes
// the response into out (which must embed slackOK), erroring on ok=false.
func (c *httpClient) callJSON(ctx context.Context, token, method string, body map[string]any, out okResponse) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	return c.do(req, method, out)
}

// callForm POSTs a form-encoded body (for the few methods that require it, e.g.
// users.lookupByEmail).
func (c *httpClient) callForm(ctx context.Context, token, method string, form url.Values, out okResponse) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, method, out)
}

func (c *httpClient) do(req *http.Request, method string, out okResponse) error {
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("slackapi: %s: %w", method, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("slackapi: %s: decode: %w", method, err)
	}
	if !out.ok() {
		return fmt.Errorf("slackapi: %s: %s", method, out.errMsg())
	}
	return nil
}

// okResponse is implemented by every decoded response (via the embedded
// slackOK) so do() can check the envelope generically.
type okResponse interface {
	ok() bool
	errMsg() string
}

func (o slackOK) ok() bool       { return o.OK }
func (o slackOK) errMsg() string { return o.Error }
