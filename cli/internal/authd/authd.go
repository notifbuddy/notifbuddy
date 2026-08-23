package authd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const ClientID = "notifbuddy-cli"

var (
	ErrAccessDenied = errors.New("the sign-in request was denied")
	ErrExpired      = errors.New("the sign-in request expired — run `notifbuddy login` again")
)

type Client struct {
	BaseURL string
	hc      *http.Client
}

func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, hc: &http.Client{Timeout: 15 * time.Second}}
}

type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func (c *Client) StartDeviceFlow(ctx context.Context) (*DeviceCode, error) {
	var out DeviceCode
	err := c.post(ctx, "", "/api/auth/device/code", map[string]any{"client_id": ClientID}, &out)
	if err != nil {
		return nil, fmt.Errorf("start device flow: %w", err)
	}
	if out.DeviceCode == "" || out.UserCode == "" {
		return nil, errors.New("start device flow: empty response from auth server")
	}
	return &out, nil
}

func (c *Client) PollForToken(ctx context.Context, dc *DeviceCode) (string, error) {
	interval := time.Duration(max(dc.Interval, 5)) * time.Second
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			return "", ErrExpired
		}
		var out struct {
			AccessToken string `json:"access_token"`
		}
		err := c.post(ctx, "", "/api/auth/device/token", map[string]any{
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
			"device_code": dc.DeviceCode,
			"client_id":   ClientID,
		}, &out)
		if err == nil && out.AccessToken != "" {
			return out.AccessToken, nil
		}
		var oe oauthError
		if errors.As(err, &oe) {
			switch oe.Code {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5 * time.Second
				continue
			case "access_denied":
				return "", ErrAccessDenied
			case "expired_token":
				return "", ErrExpired
			}
		}
		if err != nil {
			return "", fmt.Errorf("device token: %w", err)
		}
	}
}

type Session struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
	Session struct {
		ActiveOrganizationID string `json:"activeOrganizationId"`
	} `json:"session"`
}

func (c *Client) GetSession(ctx context.Context, token string) (*Session, error) {
	var out Session
	if err := c.get(ctx, token, "/api/auth/get-session", &out); err != nil {
		return nil, err
	}
	if out.User.ID == "" {
		return nil, errors.New("session invalid or expired — run `notifbuddy login`")
	}
	return &out, nil
}

type Org struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (c *Client) ListOrganizations(ctx context.Context, token string) ([]Org, error) {
	var out []Org
	if err := c.get(ctx, token, "/api/auth/organization/list", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SetActiveOrganization(ctx context.Context, token, orgID string) error {
	return c.post(ctx, token, "/api/auth/organization/set-active", map[string]any{"organizationId": orgID}, nil)
}

func (c *Client) SignOut(ctx context.Context, token string) error {
	return c.post(ctx, token, "/api/auth/sign-out", map[string]any{}, nil)
}

type oauthError struct {
	Code    string `json:"error"`
	Message string `json:"error_description"`
}

func (e oauthError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func (c *Client) get(ctx context.Context, token, path string, out any) error {
	return c.do(ctx, http.MethodGet, token, path, nil, out)
}

func (c *Client) post(ctx context.Context, token, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, token, path, body, out)
}

func (c *Client) do(ctx context.Context, method, token, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var oe oauthError
		if json.Unmarshal(raw, &oe) == nil && oe.Code != "" {
			return oe
		}
		var em struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &em) == nil && em.Message != "" {
			return errors.New(em.Message)
		}
		return fmt.Errorf("%s %s: status %d", method, path, resp.StatusCode)
	}
	if out != nil && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s %s: decode: %w", method, path, err)
		}
	}
	return nil
}
