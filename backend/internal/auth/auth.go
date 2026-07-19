// Package auth validates sessions and manages organizations against authd,
// the Better Auth service (authd/ in this repo). The browser talks to authd
// directly for sign-in/sign-up/logout; this package's job is (a) resolving
// the request's session user from the forwarded cookie and (b) proxying
// org/member/invitation operations, always on behalf of the caller — every
// authd call carries the caller's own cookie, never a service credential.
package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"xolo/backend/internal/config"
)

// Service talks to authd. Safe for concurrent use.
type Service struct {
	baseURL      string // authd base URL, e.g. http://localhost:8787
	hc           *http.Client
	postLoginURL string

	// Session cache: get-session (+ active-member) per request would double
	// every API call's latency, so resolved users are cached briefly, keyed by
	// a hash of the Cookie header. 60s keeps revocation lag negligible.
	mu    sync.Mutex
	cache map[string]cachedUser
}

type cachedUser struct {
	user *SessionUser
	exp  time.Time
}

const sessionCacheTTL = 60 * time.Second

// ctxKey / httpCtxKey mirror the previous WorkOS implementation: WithSession
// stashes the resolved user and the raw HTTP pair for ogen handlers.
type ctxKey struct{}
type httpCtxKey struct{}

// HTTPPair bundles the writer and request for handlers that need raw HTTP access.
type HTTPPair struct {
	W http.ResponseWriter
	R *http.Request
}

// SessionUser is the minimal user info the JSON handlers need. It is nil in the
// context when the request is unauthenticated. OrgID/Role describe the active
// organization (empty when the user has none).
type SessionUser struct {
	ID                string
	Email             string
	FirstName         string
	LastName          string
	ProfilePictureURL string
	OrgID             string
	Role              string
}

// Role slugs the product recognizes (Better Auth org plugin defaults, plus
// viewer kept for API compatibility).
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleViewer = "viewer"
	// roleOwner is Better Auth's creator role; surfaced as admin-equivalent.
	roleOwner = "owner"
)

// Errors mirrored from the previous implementation; handlers map them to
// status codes.
var (
	ErrMembershipNotFound = errors.New("organization membership not found")
	ErrOwnRole            = errors.New("cannot change your own role")
	ErrInvitationNotFound = errors.New("invitation not found")
)

// UserMessageError wraps an authd-provided message that is safe to show to the
// end user.
type UserMessageError struct{ Msg string }

func (e UserMessageError) Error() string { return e.Msg }

// New builds the authd client from the loaded Config.
func New(cfg config.Config) *Service {
	return &Service{
		baseURL:      strings.TrimRight(cfg.Auth.BaseURL, "/"),
		hc:           &http.Client{Timeout: 10 * time.Second},
		postLoginURL: cfg.App.PostLoginURL,
		cache:        map[string]cachedUser{},
	}
}

// --- session middleware ------------------------------------------------------

// WithSession resolves the request's session via authd and stashes the user
// (nil when unauthenticated) plus the raw HTTP pair in the context. Handlers
// decide whether nil means 401 — enforcement stays in one place.
func (a *Service) WithSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := a.resolveUser(r)
		ctx := context.WithValue(r.Context(), ctxKey{}, user)
		ctx = context.WithValue(ctx, httpCtxKey{}, HTTPPair{W: w, R: r})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext returns the authenticated user stashed by WithSession, or nil.
func UserFromContext(ctx context.Context) *SessionUser {
	u, _ := ctx.Value(ctxKey{}).(*SessionUser)
	return u
}

// HTTPFromContext returns the raw HTTP pair stashed by WithSession.
func HTTPFromContext(ctx context.Context) (HTTPPair, bool) {
	p, ok := ctx.Value(httpCtxKey{}).(HTTPPair)
	return p, ok
}

// OrgUserFromRequest returns the active organization id and user id for a
// request's session (both empty if unauthenticated or org-less).
func OrgUserFromRequest(r *http.Request) (orgID, userID string) {
	if u := UserFromContext(r.Context()); u != nil {
		return u.OrgID, u.ID
	}
	return "", ""
}

// resolveUser turns the request's cookies into a SessionUser via authd,
// consulting the short-lived cache first.
func (a *Service) resolveUser(r *http.Request) *SessionUser {
	cookie := r.Header.Get("Cookie")
	if cookie == "" {
		return nil
	}
	key := cacheKey(cookie)

	// The SPA sends Cache-Control: no-cache right after changing the active
	// organization in authd (a change this backend can't observe) — honor it
	// by skipping the cached view for this request.
	if r.Header.Get("Cache-Control") != "no-cache" {
		a.mu.Lock()
		if c, ok := a.cache[key]; ok && time.Now().Before(c.exp) {
			a.mu.Unlock()
			return c.user
		}
		a.mu.Unlock()
	}

	user := a.fetchUser(r.Context(), cookie)

	a.mu.Lock()
	// Opportunistic eviction keeps the map from growing without a janitor —
	// there is deliberately no background goroutine (scale-to-zero rule).
	if len(a.cache) > 4096 {
		a.cache = map[string]cachedUser{}
	}
	a.cache[key] = cachedUser{user: user, exp: time.Now().Add(sessionCacheTTL)}
	a.mu.Unlock()
	return user
}

func cacheKey(cookie string) string {
	sum := sha256.Sum256([]byte(cookie))
	return hex.EncodeToString(sum[:])
}

// getSessionResponse is the subset of authd's get-session payload we read.
type getSessionResponse struct {
	Session struct {
		ActiveOrganizationID string `json:"activeOrganizationId"`
	} `json:"session"`
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
		Image string `json:"image"`
	} `json:"user"`
}

func (a *Service) fetchUser(ctx context.Context, cookie string) *SessionUser {
	var sess getSessionResponse
	if err := a.call(ctx, cookie, http.MethodGet, "/api/auth/get-session", nil, &sess); err != nil {
		return nil // unauthenticated or authd unreachable — request proceeds anonymous
	}
	if sess.User.ID == "" {
		return nil
	}
	first, last := splitName(sess.User.Name)
	su := &SessionUser{
		ID:                sess.User.ID,
		Email:             sess.User.Email,
		FirstName:         first,
		LastName:          last,
		ProfilePictureURL: sess.User.Image,
		OrgID:             sess.Session.ActiveOrganizationID,
	}
	if su.OrgID != "" {
		var member struct {
			Role string `json:"role"`
		}
		if err := a.call(ctx, cookie, http.MethodGet, "/api/auth/organization/get-active-member", nil, &member); err == nil {
			su.Role = normalizeRole(member.Role)
		}
	}
	return su
}

// --- organizations -----------------------------------------------------------

// OrgMembership is the trimmed view of one of a user's organizations.
type OrgMembership struct {
	ID   string
	Name string
	Role string
}

type orgListEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListUserOrganizations returns the organizations the session's user belongs
// to (up to limit). The userID parameter is kept for handler compatibility;
// authd scopes the list by the forwarded cookie. Best-effort: nil on error so
// /me still works without the org list.
func (a *Service) ListUserOrganizations(ctx context.Context, _ string, limit int) []OrgMembership {
	cookie, ok := cookieFromContext(ctx)
	if !ok {
		return nil
	}
	var orgs []orgListEntry
	if err := a.call(ctx, cookie, http.MethodGet, "/api/auth/organization/list", nil, &orgs); err != nil {
		slog.ErrorContext(ctx, "auth: list user organizations failed", "error", err)
		return nil
	}
	out := make([]OrgMembership, 0, len(orgs))
	for i, o := range orgs {
		if i >= limit {
			break
		}
		out = append(out, OrgMembership{ID: o.ID, Name: o.Name})
	}
	return out
}

// CreateOrganizationForUser creates an organization for the signed-in user and
// makes it their active organization. Better Auth adds the creator as owner.
// The returned SessionUser carries the new OrgID.
func (a *Service) CreateOrganizationForUser(_ http.ResponseWriter, r *http.Request, userID, name string) (*SessionUser, error) {
	if userID == "" {
		return nil, errors.New("unauthenticated")
	}
	cookie := r.Header.Get("Cookie")
	if cookie == "" {
		return nil, errors.New("no session cookie")
	}
	ctx := r.Context()

	origin := r.Header.Get("Origin")
	var created struct {
		ID string `json:"id"`
	}
	body := map[string]any{"name": name, "slug": slugify(name)}
	if err := a.callWithOrigin(ctx, cookie, origin, http.MethodPost, "/api/auth/organization/create", body, &created); err != nil {
		slog.ErrorContext(ctx, "auth: create organization failed", "error", err)
		return nil, err
	}
	if err := a.callWithOrigin(ctx, cookie, origin, http.MethodPost, "/api/auth/organization/set-active",
		map[string]any{"organizationId": created.ID}, nil); err != nil {
		slog.ErrorContext(ctx, "auth: set active organization failed", "org_id", created.ID, "error", err)
		return nil, err
	}
	a.invalidate(cookie)

	su := a.fetchUser(ctx, cookie)
	if su == nil {
		return nil, errors.New("session lookup failed after organization create")
	}
	return su, nil
}

// fullOrganization is the subset of get-full-organization we read.
type fullOrganization struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Members []struct {
		ID     string `json:"id"`
		UserID string `json:"userId"`
		Role   string `json:"role"`
		User   struct {
			Email string `json:"email"`
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"user"`
	} `json:"members"`
}

func (a *Service) fullOrganization(ctx context.Context, cookie, orgID string) (*fullOrganization, error) {
	var org fullOrganization
	path := "/api/auth/organization/get-full-organization?organizationId=" + url.QueryEscape(orgID)
	if err := a.call(ctx, cookie, http.MethodGet, path, nil, &org); err != nil {
		return nil, err
	}
	return &org, nil
}

// GetOrganizationName returns the organization's display name.
func (a *Service) GetOrganizationName(ctx context.Context, orgID string) (string, error) {
	cookie, ok := cookieFromContext(ctx)
	if !ok {
		return "", errors.New("unauthenticated")
	}
	org, err := a.fullOrganization(ctx, cookie, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "auth: get organization failed", "org_id", orgID, "error", err)
		return "", err
	}
	return org.Name, nil
}

// UpdateOrganizationName renames the organization and returns the stored name.
func (a *Service) UpdateOrganizationName(ctx context.Context, orgID, name string) (string, error) {
	cookie, ok := cookieFromContext(ctx)
	if !ok {
		return "", errors.New("unauthenticated")
	}
	var resp struct {
		Name string `json:"name"`
	}
	body := map[string]any{"organizationId": orgID, "data": map[string]any{"name": name}}
	if err := a.call(ctx, cookie, http.MethodPost, "/api/auth/organization/update", body, &resp); err != nil {
		slog.ErrorContext(ctx, "auth: update organization name failed", "org_id", orgID, "error", err)
		return "", err
	}
	return resp.Name, nil
}

// --- members -----------------------------------------------------------------

// Member is the trimmed view of one organization member.
type Member struct {
	ID                string
	UserID            string
	Email             string
	FirstName         string
	LastName          string
	Role              string
	ProfilePictureURL string
}

// ListOrganizationMembers returns the members of an organization (up to limit).
func (a *Service) ListOrganizationMembers(ctx context.Context, orgID string, limit int) ([]Member, error) {
	cookie, ok := cookieFromContext(ctx)
	if !ok {
		return nil, errors.New("unauthenticated")
	}
	org, err := a.fullOrganization(ctx, cookie, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "auth: list organization members failed", "org_id", orgID, "error", err)
		return nil, err
	}
	var out []Member
	for i, m := range org.Members {
		if i >= limit {
			break
		}
		first, last := splitName(m.User.Name)
		out = append(out, Member{
			ID:                m.ID,
			UserID:            m.UserID,
			Email:             m.User.Email,
			FirstName:         first,
			LastName:          last,
			Role:              normalizeRole(m.Role),
			ProfilePictureURL: m.User.Image,
		})
	}
	return out, nil
}

// UpdateOrganizationMemberRole sets one membership's role. The membership must
// belong to orgID and must not be the caller's own.
func (a *Service) UpdateOrganizationMemberRole(ctx context.Context, orgID, callerUserID, membershipID, roleSlug string) (Member, error) {
	cookie, ok := cookieFromContext(ctx)
	if !ok {
		return Member{}, errors.New("unauthenticated")
	}
	org, err := a.fullOrganization(ctx, cookie, orgID)
	if err != nil {
		return Member{}, ErrMembershipNotFound
	}
	var target *Member
	for _, m := range org.Members {
		if m.ID == membershipID {
			first, last := splitName(m.User.Name)
			target = &Member{ID: m.ID, UserID: m.UserID, Email: m.User.Email,
				FirstName: first, LastName: last, ProfilePictureURL: m.User.Image}
			break
		}
	}
	if target == nil {
		return Member{}, ErrMembershipNotFound
	}
	if target.UserID == callerUserID {
		return Member{}, ErrOwnRole
	}
	body := map[string]any{"organizationId": orgID, "memberId": membershipID, "role": roleSlug}
	if err := a.call(ctx, cookie, http.MethodPost, "/api/auth/organization/update-member-role", body, nil); err != nil {
		slog.ErrorContext(ctx, "auth: update membership role failed", "membership_id", membershipID, "error", err)
		return Member{}, err
	}
	target.Role = roleSlug
	return *target, nil
}

// --- invitations -------------------------------------------------------------

// Invitation is the trimmed invitation shape the handlers return.
type Invitation struct {
	ID        string
	Email     string
	State     string
	ExpiresAt string
	Role      string
}

type invitationEntry struct {
	ID             string `json:"id"`
	Email          string `json:"email"`
	Status         string `json:"status"`
	ExpiresAt      string `json:"expiresAt"`
	Role           string `json:"role"`
	OrganizationID string `json:"organizationId"`
}

func (i invitationEntry) toInvitation() Invitation {
	return Invitation{ID: i.ID, Email: i.Email, State: i.Status, ExpiresAt: i.ExpiresAt, Role: normalizeRole(i.Role)}
}

// SendInvitation invites an email to the organization.
func (a *Service) SendInvitation(ctx context.Context, email, orgID, role, _ string) (*Invitation, error) {
	cookie, ok := cookieFromContext(ctx)
	if !ok {
		return nil, errors.New("unauthenticated")
	}
	if role == "" {
		role = RoleMember
	}
	var inv invitationEntry
	body := map[string]any{"organizationId": orgID, "email": email, "role": role}
	if err := a.call(ctx, cookie, http.MethodPost, "/api/auth/organization/invite-member", body, &inv); err != nil {
		slog.ErrorContext(ctx, "auth: send invitation failed", "error", err)
		return nil, err
	}
	out := inv.toInvitation()
	return &out, nil
}

// ListInvitations returns up to limit invitations for an organization.
func (a *Service) ListInvitations(ctx context.Context, orgID string, limit int) ([]Invitation, error) {
	cookie, ok := cookieFromContext(ctx)
	if !ok {
		return nil, errors.New("unauthenticated")
	}
	var invs []invitationEntry
	path := "/api/auth/organization/list-invitations?organizationId=" + url.QueryEscape(orgID)
	if err := a.call(ctx, cookie, http.MethodGet, path, nil, &invs); err != nil {
		slog.ErrorContext(ctx, "auth: list invitations failed", "error", err)
		return nil, err
	}
	var out []Invitation
	for i, inv := range invs {
		if i >= limit {
			break
		}
		out = append(out, inv.toInvitation())
	}
	return out, nil
}

// RevokeInvitation cancels one of an organization's invitations.
func (a *Service) RevokeInvitation(ctx context.Context, orgID, invitationID string) (*Invitation, error) {
	cookie, ok := cookieFromContext(ctx)
	if !ok {
		return nil, errors.New("unauthenticated")
	}
	// Scope check: the invitation must belong to orgID.
	invs, err := a.ListInvitations(ctx, orgID, 1000)
	if err != nil {
		return nil, err
	}
	found := false
	for _, inv := range invs {
		if inv.ID == invitationID {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrInvitationNotFound
	}
	var cancelled struct {
		Invitation invitationEntry `json:"invitation"`
	}
	body := map[string]any{"invitationId": invitationID}
	if err := a.call(ctx, cookie, http.MethodPost, "/api/auth/organization/cancel-invitation", body, &cancelled); err != nil {
		slog.ErrorContext(ctx, "auth: cancel invitation failed", "invitation_id", invitationID, "error", err)
		return nil, err
	}
	out := cancelled.Invitation.toInvitation()
	if out.ID == "" {
		out = Invitation{ID: invitationID, State: "canceled"}
	}
	return &out, nil
}

// --- plumbing ----------------------------------------------------------------

// call performs an authd request with the caller's cookie, decoding a JSON
// response into out (may be nil). authd error bodies ({"message": ...}) become
// UserMessageError so handlers can surface them.
func (a *Service) call(ctx context.Context, cookie, method, path string, body any, out any) error {
	return a.callWithOrigin(ctx, cookie, originFromContext(ctx), method, path, body, out)
}

// callWithOrigin is call with an explicit Origin header (used where the
// origin comes from the raw request rather than the context).
func (a *Service) callWithOrigin(ctx context.Context, cookie, origin, method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", cookie)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return fmt.Errorf("auth: authd %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("auth: authd %s: read: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Message != "" {
			return UserMessageError{Msg: e.Message}
		}
		return fmt.Errorf("auth: authd %s: status %d", path, resp.StatusCode)
	}
	if out != nil && len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("auth: authd %s: decode: %w", path, err)
		}
	}
	return nil
}

// invalidate drops the cached session for a cookie (after org changes that
// alter the active organization).
func (a *Service) invalidate(cookie string) {
	a.mu.Lock()
	delete(a.cache, cacheKey(cookie))
	a.mu.Unlock()
}

// cookieFromContext extracts the request's Cookie header via the HTTP pair
// stashed by WithSession.
func cookieFromContext(ctx context.Context) (string, bool) {
	p, ok := HTTPFromContext(ctx)
	if !ok || p.R == nil {
		return "", false
	}
	c := p.R.Header.Get("Cookie")
	return c, c != ""
}

// originFromContext extracts the request's Origin header (the SPA origin).
// Better Auth's CSRF check requires a trusted Origin on state-changing calls;
// forwarding the browser's own Origin keeps that check meaningful.
func originFromContext(ctx context.Context) string {
	p, ok := HTTPFromContext(ctx)
	if !ok || p.R == nil {
		return ""
	}
	return p.R.Header.Get("Origin")
}

// normalizeRole maps Better Auth's owner role onto admin — the product's role
// vocabulary predates the migration and treats the org creator as admin.
func normalizeRole(role string) string {
	if role == roleOwner {
		return RoleAdmin
	}
	return role
}

// splitName splits a display name into first/last on the first space.
func splitName(name string) (first, last string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	parts := strings.SplitN(name, " ", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// slugify derives a URL-safe org slug from the display name, with a random
// suffix so equal names never collide (Better Auth requires unique slugs).
func slugify(name string) string {
	s := slugStrip.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if len(s) > 30 {
		s = s[:30]
	}
	if s == "" {
		s = "org"
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return s + "-" + hex.EncodeToString(b)
}
