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
	"net/http"
	"net/url"
	"strings"
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
}

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
}

// httpClient is the default Client, talking to https://slack.com/api. It is
// stateless apart from the underlying *http.Client.
type httpClient struct {
	hc      *http.Client
	baseURL string
}

// New returns the default HTTP-backed Slack client.
func New() Client { return &httpClient{hc: http.DefaultClient, baseURL: "https://slack.com/api"} }

// NewWithHTTP returns a client with a custom base URL and *http.Client, for
// tests (point baseURL at an httptest server).
func NewWithHTTP(baseURL string, hc *http.Client) Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &httpClient{hc: hc, baseURL: strings.TrimRight(baseURL, "/")}
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
	var out struct {
		slackOK
		TS string `json:"ts"`
	}
	if err := c.callJSON(ctx, token, "chat.postMessage", body, &out); err != nil {
		return "", err
	}
	return out.TS, nil
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
