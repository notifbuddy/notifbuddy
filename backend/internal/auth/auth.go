package auth

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	workos "github.com/workos/workos-go/v9"

	"xolo/backend/internal/config"
)

// sessionCookieName is the name of the HttpOnly cookie holding the sealed
// WorkOS session (access token + refresh token + user, encrypted with the
// cookie password). The browser never sees its contents.
const sessionCookieName = "wos_session"

// pendingCookieName holds the WorkOS pending_authentication_token (sealed) while
// the user completes email verification. It is short-lived and cleared once
// verification succeeds.
const pendingCookieName = "wos_pending"

// pendingCookieMaxAge bounds how long a half-finished verification can sit
// around. WorkOS pending tokens are short-lived anyway; this just caps the
// cookie.
const pendingCookieMaxAge = 15 * 60 // seconds

// orgSelectCookieName holds the sealed org-selection state (pending token + the
// organizations the user may choose between) while they pick an organization.
const orgSelectCookieName = "wos_org_select"

// OrgChoice is one selectable organization, surfaced to the SPA's picker.
type OrgChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// orgSelectState is sealed into the org-selection cookie: the WorkOS pending
// token plus the organizations to choose from.
type orgSelectState struct {
	PendingToken  string      `json:"pending_token"`
	Organizations []OrgChoice `json:"organizations"`
}

// Service holds everything the auth flow needs, resolved once at startup.
type Service struct {
	client         *workos.Client
	redirectURI    string // WORKOS_REDIRECT_URI — must match the dashboard
	cookiePassword string // ≥32 chars; seals/unseals the session cookie
	postLoginURL   string // where to send the browser after a successful login
	secureCookies  bool   // Secure flag on the session cookie (off only for plain-HTTP testing)
	// loginProvider, when set, sends the user straight to a specific AuthKit
	// provider (e.g. "GitHubOAuth") instead of rendering AuthKit's method
	// selector. Required when only a single social connection is enabled, since
	// the bare authorize endpoint otherwise can't resolve a connection. Empty
	// means "let AuthKit show its hosted selector".
	loginProvider string
}

// ctxKey is an unexported context key type so our session value can't collide
// with anything else stored in the request context.
type ctxKey struct{}

// httpCtxKey keys the raw HTTP writer/request pair in the context. Some ogen
// handlers (VerifyEmail) need to read a cookie and set the session cookie, but
// ogen only hands handlers a context — so WithSession stashes the HTTP objects
// here for those handlers to retrieve via HTTPFromContext.
type httpCtxKey struct{}

// HTTPPair bundles the writer and request for handlers that need raw HTTP access.
type HTTPPair struct {
	W http.ResponseWriter
	R *http.Request
}

// SessionUser is the minimal user info the JSON handlers need. It is nil in the
// context when the request is unauthenticated. OrgID/Role describe the
// organization the current session is scoped to (empty when the user has no
// active organization).
type SessionUser struct {
	ID                string
	Email             string
	FirstName         string
	LastName          string
	ProfilePictureURL string // GitHub avatar for GitHub logins; WorkOS captures it at sign-in
	OrgID             string
	Role              string
}

// New builds the WorkOS client from the loaded Config. Secret fields
// (API key, cookie password) have already been resolved from their env
// references during config load.
func New(cfg config.Config) *Service {
	return &Service{
		client:         workos.NewClient(cfg.WorkOS.APIKey, workos.WithClientID(cfg.WorkOS.ClientID)),
		redirectURI:    cfg.WorkOS.RedirectURI,
		cookiePassword: cfg.WorkOS.CookiePassword,
		postLoginURL:   cfg.App.PostLoginURL,
		// Secure cookies work over http://localhost (browsers exempt localhost),
		// so we keep them on unless app.insecure_cookies is set (only needed for
		// plain-HTTP testing on a non-localhost host).
		secureCookies: !cfg.App.InsecureCookies,
		loginProvider: cfg.WorkOS.LoginProvider,
	}
}

// HandleLogin redirects the browser to the WorkOS AuthKit hosted login page.
// AuthKit owns the login screen (email/password, social, SSO — configured in
// the WorkOS dashboard); we only kick off the flow.
func (a *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	params := workos.AuthKitAuthorizationURLParams{
		RedirectURI: a.redirectURI,
	}
	// Send the user straight to a specific provider (e.g. GitHub) when
	// configured, instead of AuthKit's selector.
	if a.loginProvider != "" {
		params.Provider = &a.loginProvider
	}
	// Accept-on-login: if the user came from an Invitation link, carry the token
	// through AuthKit's `state` so it round-trips to our callback, which feeds it
	// into AuthenticateWithCode to attach the invited organization.
	if invToken := r.URL.Query().Get("invitation_token"); invToken != "" {
		params.State = &invToken
	}
	url, err := a.client.GetAuthKitAuthorizationURL(params)
	if err != nil {
		log.Printf("auth: build authorization URL: %v", err)
		http.Error(w, "failed to start login", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

// HandleCallback is the WORKOS_REDIRECT_URI target. AuthKit redirects here with
// a `code` query param after the user signs in. We exchange it for tokens, seal
// them into the session cookie, and bounce the browser back to the SPA.
func (a *Service) HandleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		// AuthKit returns error/error_description on failure (e.g. user cancelled).
		if e := r.URL.Query().Get("error"); e != "" {
			log.Printf("auth: callback error: %s — %s", e, r.URL.Query().Get("error_description"))
		}
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	params := &workos.UserManagementAuthenticateWithCodeParams{Code: code}
	// Accept-on-login: the Invitation token arrives either directly as
	// ?invitation_token= (WorkOS Invitation links) or echoed back in ?state=
	// (when we kicked off login via /auth/login?invitation_token=). Either way,
	// passing it attaches the new session to the invited organization.
	if invToken := r.URL.Query().Get("invitation_token"); invToken != "" {
		params.InvitationToken = &invToken
	} else if state := r.URL.Query().Get("state"); state != "" {
		params.InvitationToken = &state
	}

	resp, err := a.client.UserManagement().AuthenticateWithCode(r.Context(), params)
	if err != nil {
		// Expected branches that need extra steps rather than a hard failure.
		var emailErr *workos.EmailVerificationRequiredError
		if errors.As(err, &emailErr) {
			// GitHub OAuth users land unverified: WorkOS emails a code.
			a.startEmailVerification(w, r, emailErr.PendingAuthenticationToken)
			return
		}
		var orgErr *workos.OrganizationSelectionRequiredError
		if errors.As(err, &orgErr) {
			// User belongs to multiple orgs and must choose one.
			a.startOrgSelection(w, r, orgErr.PendingAuthenticationToken, orgErr.Organizations)
			return
		}
		// Any other error: log the structured details and fail.
		var apiErr *workos.APIError
		if errors.As(err, &apiErr) {
			log.Printf("auth: exchange code failed: status=%d code=%q message=%q error=%q error_description=%q request_id=%q body=%s",
				apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.ErrorCode, apiErr.ErrorDescription, apiErr.RequestID, apiErr.RawBody)
		} else {
			log.Printf("auth: exchange code failed: %v", err)
		}
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	if _, err := a.establishSession(w, resp); err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, a.postLoginURL, http.StatusFound)
}

// startEmailVerification seals the pending authentication token into a
// short-lived cookie and redirects the browser to the SPA with ?verify=1 so it
// shows the code-entry step. WorkOS has already emailed the code.
func (a *Service) startEmailVerification(w http.ResponseWriter, r *http.Request, pendingToken string) {
	if pendingToken == "" {
		log.Printf("auth: email_verification_required but no pending token returned")
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	sealed, err := workos.Seal(pendingToken, a.cookiePassword)
	if err != nil {
		log.Printf("auth: seal pending token: %v", err)
		http.Error(w, "failed to start verification", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     pendingCookieName,
		Value:    sealed,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   pendingCookieMaxAge,
	})
	http.Redirect(w, r, a.postLoginURL+"/?verify=1", http.StatusFound)
}

// establishSession seals an authenticated WorkOS response into the session
// cookie and returns the resulting session user (with org/role derived from the
// new token's claims). Shared by the OAuth callback, email-verification, and
// org-selection completions. It only sets the cookie and returns an error; the
// caller decides how to report failure (redirect vs. JSON), so this never
// writes to w on error.
func (a *Service) establishSession(w http.ResponseWriter, resp *workos.AuthenticateResponse) (*SessionUser, error) {
	sealed, err := workos.SealSessionFromAuthResponse(
		resp.AccessToken, resp.RefreshToken, resp.User, resp.Impersonator, a.cookiePassword)
	if err != nil {
		log.Printf("auth: seal session: %v", err)
		return nil, err
	}
	a.setSessionCookie(w, sealed)

	// Derive org_id/role from the freshly issued token's claims by reading back
	// the sealed session — keeps org/role extraction in one place.
	su := toSessionUser(resp.User, deref(resp.OrganizationID), "")
	if reauth, err := workos.AuthenticateSession(sealed, a.cookiePassword); err == nil && reauth.Authenticated && su != nil {
		su.OrgID = reauth.OrganizationID
		su.Role = reauth.Role
	}
	return su, nil
}

// CompleteEmailVerification finishes a login that was gated on email
// verification. It reads the sealed pending token from the request cookie,
// exchanges it plus the user-supplied code for a session, sets the session
// cookie, clears the pending cookie, and returns the authenticated user.
// Returns nil + an error on any failure (bad/expired code, missing pending
// cookie) — the caller maps that to a 401.
func (a *Service) CompleteEmailVerification(w http.ResponseWriter, r *http.Request, code string) (*SessionUser, error) {
	c, err := r.Cookie(pendingCookieName)
	if err != nil || c.Value == "" {
		return nil, errors.New("no pending verification")
	}
	pendingToken, err := workos.Unseal[string](c.Value, a.cookiePassword)
	if err != nil {
		return nil, errors.New("invalid pending verification cookie")
	}

	resp, err := a.client.UserManagement().AuthenticateWithEmailVerification(r.Context(),
		&workos.UserManagementAuthenticateWithEmailVerificationParams{
			Code:                       code,
			PendingAuthenticationToken: pendingToken,
		})
	if err != nil {
		var apiErr *workos.APIError
		if errors.As(err, &apiErr) {
			log.Printf("auth: verify email failed: status=%d code=%q message=%q request_id=%q",
				apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.RequestID)
		} else {
			log.Printf("auth: verify email failed: %v", err)
		}
		return nil, errors.New("verification failed")
	}

	su, err := a.establishSession(w, resp)
	if err != nil {
		return nil, err
	}
	a.clearPendingCookie(w)
	return su, nil
}

// clearPendingCookie expires the pending-verification cookie.
func (a *Service) clearPendingCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     pendingCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// startOrgSelection seals the pending token plus the org choices into a
// short-lived cookie and redirects the SPA to its org-picker step. The SPA reads
// the choices via GET /auth/pending-orgs and POSTs the pick to /auth/select-org.
func (a *Service) startOrgSelection(w http.ResponseWriter, r *http.Request, pendingToken string, orgs []workos.PendingAuthenticationOrganization) {
	if pendingToken == "" || len(orgs) == 0 {
		log.Printf("auth: organization_selection_required but missing token/orgs")
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	state := orgSelectState{PendingToken: pendingToken}
	for _, o := range orgs {
		state.Organizations = append(state.Organizations, OrgChoice{ID: o.ID, Name: o.Name})
	}
	sealed, err := workos.Seal(state, a.cookiePassword)
	if err != nil {
		log.Printf("auth: seal org-selection state: %v", err)
		http.Error(w, "failed to start org selection", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     orgSelectCookieName,
		Value:    sealed,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   pendingCookieMaxAge,
	})
	http.Redirect(w, r, a.postLoginURL+"/?select-org=1", http.StatusFound)
}

// PendingOrgChoices returns the organizations the user may choose between, read
// from the sealed org-selection cookie. Returns nil if there's no pending
// selection.
func (a *Service) PendingOrgChoices(r *http.Request) []OrgChoice {
	state, ok := a.readOrgSelectState(r)
	if !ok {
		return nil
	}
	return state.Organizations
}

// readOrgSelectState unseals the org-selection cookie.
func (a *Service) readOrgSelectState(r *http.Request) (orgSelectState, bool) {
	c, err := r.Cookie(orgSelectCookieName)
	if err != nil || c.Value == "" {
		return orgSelectState{}, false
	}
	state, err := workos.Unseal[orgSelectState](c.Value, a.cookiePassword)
	if err != nil {
		return orgSelectState{}, false
	}
	return state, true
}

// CompleteOrgSelection finishes a login gated on org selection: it exchanges the
// chosen organization plus the stashed pending token for a session, sets the
// session cookie, clears the selection cookie, and returns the user. Returns nil
// + error on any failure (no pending selection, invalid token/org).
func (a *Service) CompleteOrgSelection(w http.ResponseWriter, r *http.Request, organizationID string) (*SessionUser, error) {
	state, ok := a.readOrgSelectState(r)
	if !ok {
		return nil, errors.New("no pending organization selection")
	}
	// Guard: the chosen org must be one of the offered choices.
	valid := false
	for _, o := range state.Organizations {
		if o.ID == organizationID {
			valid = true
			break
		}
	}
	if !valid {
		return nil, errors.New("organization not in pending selection")
	}

	resp, err := a.client.UserManagement().AuthenticateWithOrganizationSelection(r.Context(),
		&workos.UserManagementAuthenticateWithOrganizationSelectionParams{
			PendingAuthenticationToken: state.PendingToken,
			OrganizationID:             organizationID,
		})
	if err != nil {
		var apiErr *workos.APIError
		if errors.As(err, &apiErr) {
			log.Printf("auth: org selection failed: status=%d code=%q message=%q request_id=%q",
				apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.RequestID)
		} else {
			log.Printf("auth: org selection failed: %v", err)
		}
		return nil, errors.New("organization selection failed")
	}

	su, err := a.establishSession(w, resp)
	if err != nil {
		return nil, err
	}
	a.clearOrgSelectCookie(w)
	return su, nil
}

// clearOrgSelectCookie expires the org-selection cookie.
func (a *Service) clearOrgSelectCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     orgSelectCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// OrgMembership is the trimmed view of one of a user's organizations.
type OrgMembership struct {
	ID   string
	Name string
	Role string
}

// ListUserOrganizations returns the active organizations a user belongs to (up
// to `limit`), for surfacing in /me. Best-effort: returns nil on error so /me
// still works without the org list.
func (a *Service) ListUserOrganizations(ctx context.Context, userID string, limit int) []OrgMembership {
	status := workos.UserManagementOrganizationMembershipStatuses("active")
	it := a.client.OrganizationMembership().List(ctx, &workos.OrganizationMembershipListParams{
		UserID:   &userID,
		Statuses: []workos.UserManagementOrganizationMembershipStatuses{status},
	})
	var out []OrgMembership
	for it.Next() && len(out) < limit {
		m := it.Current()
		om := OrgMembership{ID: m.OrganizationID, Name: deref(m.OrganizationName)}
		if m.Role != nil {
			om.Role = m.Role.Slug
		}
		out = append(out, om)
	}
	if err := it.Err(); err != nil {
		log.Printf("auth: list user organizations failed: %v", err)
		return nil
	}
	return out
}

// Role slugs the product recognizes. Roles with these slugs must exist in the
// WorkOS environment (configured in the dashboard).
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleViewer = "viewer"
)

// ErrMembershipNotFound reports that a membership id does not exist within the
// given organization (including memberships that belong to another org).
var ErrMembershipNotFound = errors.New("organization membership not found")

// ErrOwnRole reports an attempt by a caller to change their own role.
var ErrOwnRole = errors.New("cannot change your own role")

// Member is the trimmed view of one organization member the handlers return.
// It joins a WorkOS organization membership (id, role) with the underlying
// user's identity (email, name).
type Member struct {
	ID                string
	UserID            string
	Email             string
	FirstName         string
	LastName          string
	Role              string
	ProfilePictureURL string
}

// ListOrganizationMembers returns the active members of an organization (up to
// `limit`). Each WorkOS membership only carries the user ID and role, so we
// resolve each member's identity (email, name) with a follow-up user lookup.
// Best-effort per member: a failed user lookup still yields a row with the IDs
// and role we already have.
func (a *Service) ListOrganizationMembers(ctx context.Context, orgID string, limit int) ([]Member, error) {
	status := workos.UserManagementOrganizationMembershipStatuses("active")
	it := a.client.OrganizationMembership().List(ctx, &workos.OrganizationMembershipListParams{
		OrganizationID: &orgID,
		Statuses:       []workos.UserManagementOrganizationMembershipStatuses{status},
	})
	var out []Member
	for it.Next() && len(out) < limit {
		out = append(out, a.memberFromMembership(ctx, *it.Current()))
	}
	if err := it.Err(); err != nil {
		log.Printf("auth: list organization members failed: %v", err)
		return nil, err
	}
	return out, nil
}

// GetOrganizationName returns the organization's display name from WorkOS.
func (a *Service) GetOrganizationName(ctx context.Context, orgID string) (string, error) {
	org, err := a.client.Organizations().Get(ctx, orgID)
	if err != nil {
		log.Printf("auth: get organization %q failed: %v", orgID, err)
		return "", err
	}
	return org.Name, nil
}

// UserMessageError wraps a WorkOS-provided message that is safe and useful to
// show to the end user (e.g. "Default test organizations cannot be updated.").
type UserMessageError struct{ Msg string }

func (e UserMessageError) Error() string { return e.Msg }

// UpdateOrganizationName renames the organization in WorkOS and returns the
// stored name. WorkOS rejections come back as a UserMessageError so handlers
// can surface the reason.
func (a *Service) UpdateOrganizationName(ctx context.Context, orgID, name string) (string, error) {
	org, err := a.client.Organizations().Update(ctx, orgID, &workos.OrganizationsUpdateParams{Name: &name})
	if err != nil {
		log.Printf("auth: update organization %q name failed: %v", orgID, err)
		var apiErr *workos.APIError
		if errors.As(err, &apiErr) && apiErr.Message != "" {
			return "", UserMessageError{Msg: apiErr.Message}
		}
		return "", err
	}
	return org.Name, nil
}

// memberFromMembership joins a WorkOS membership with the underlying user's
// identity. Best-effort: a failed user lookup still yields a Member with the
// IDs and role the membership already carries.
func (a *Service) memberFromMembership(ctx context.Context, m workos.UserOrganizationMembership) Member {
	member := Member{ID: m.ID, UserID: m.UserID}
	if m.Role != nil {
		member.Role = m.Role.Slug
	}
	if u, err := a.client.UserManagement().Get(ctx, m.UserID); err != nil {
		log.Printf("auth: resolve member user %q failed: %v", m.UserID, err)
	} else {
		member.Email = u.Email
		member.FirstName = deref(u.FirstName)
		member.LastName = deref(u.LastName)
		member.ProfilePictureURL = deref(u.ProfilePictureURL)
	}
	return member
}

// UpdateOrganizationMemberRole sets the role of one organization membership
// and returns the refreshed member. The membership must belong to orgID (one
// organization can never touch another's memberships) and must not be the
// caller's own (so an org can't accidentally demote its last admin).
func (a *Service) UpdateOrganizationMemberRole(ctx context.Context, orgID, callerUserID, membershipID, roleSlug string) (Member, error) {
	m, err := a.client.OrganizationMembership().Get(ctx, membershipID)
	if err != nil {
		log.Printf("auth: get membership %q failed: %v", membershipID, err)
		return Member{}, ErrMembershipNotFound
	}
	if m.OrganizationID != orgID {
		return Member{}, ErrMembershipNotFound
	}
	if m.UserID == callerUserID {
		return Member{}, ErrOwnRole
	}
	updated, err := a.client.OrganizationMembership().Update(ctx, membershipID, &workos.OrganizationMembershipUpdateParams{
		Role: workos.OrganizationMembershipRoleSingle{Slug: roleSlug},
	})
	if err != nil {
		log.Printf("auth: update membership %q role failed: %v", membershipID, err)
		return Member{}, err
	}
	return a.memberFromMembership(ctx, *updated), nil
}

// Invitation is the trimmed Invitation shape the handlers return.
type Invitation struct {
	ID        string
	Email     string
	State     string
	ExpiresAt string
	Role      string
}

// ErrInvitationNotFound reports that an invitation id does not exist within
// the given organization (including invitations that belong to another org).
var ErrInvitationNotFound = errors.New("invitation not found")

// SendInvitation invites an email to the given organization, optionally with a
// role, attributing the invite to the inviter. Returns the created Invitation.
func (a *Service) SendInvitation(ctx context.Context, email, orgID, role, inviterUserID string) (*Invitation, error) {
	params := &workos.UserManagementSendInvitationParams{
		Email:          email,
		OrganizationID: &orgID,
	}
	if role != "" {
		params.RoleSlug = &role
	}
	if inviterUserID != "" {
		params.InviterUserID = &inviterUserID
	}
	inv, err := a.client.UserManagement().SendInvitation(ctx, params)
	if err != nil {
		var apiErr *workos.APIError
		if errors.As(err, &apiErr) {
			log.Printf("auth: send Invitation failed: status=%d code=%q message=%q request_id=%q",
				apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.RequestID)
		} else {
			log.Printf("auth: send Invitation failed: %v", err)
		}
		return nil, err
	}
	return &Invitation{ID: inv.ID, Email: inv.Email, State: string(inv.State), ExpiresAt: inv.ExpiresAt, Role: deref(inv.RoleSlug)}, nil
}

// ListInvitations returns up to `limit` invitations for an organization.
func (a *Service) ListInvitations(ctx context.Context, orgID string, limit int) ([]Invitation, error) {
	it := a.client.UserManagement().ListInvitations(ctx,
		&workos.UserManagementListInvitationsParams{OrganizationID: &orgID})
	var out []Invitation
	for it.Next() && len(out) < limit {
		inv := it.Current()
		out = append(out, Invitation{ID: inv.ID, Email: inv.Email, State: string(inv.State), ExpiresAt: inv.ExpiresAt, Role: deref(inv.RoleSlug)})
	}
	if err := it.Err(); err != nil {
		log.Printf("auth: list invitations failed: %v", err)
		return nil, err
	}
	return out, nil
}

// RevokeInvitation revokes one of an organization's invitations so its link
// can no longer be accepted. The invitation must belong to orgID (one
// organization can never touch another's invitations).
func (a *Service) RevokeInvitation(ctx context.Context, orgID, invitationID string) (*Invitation, error) {
	inv, err := a.client.UserManagement().GetInvitation(ctx, invitationID)
	if err != nil {
		log.Printf("auth: get invitation %q failed: %v", invitationID, err)
		return nil, ErrInvitationNotFound
	}
	if deref(inv.OrganizationID) != orgID {
		return nil, ErrInvitationNotFound
	}
	revoked, err := a.client.UserManagement().RevokeInvitation(ctx, invitationID)
	if err != nil {
		log.Printf("auth: revoke invitation %q failed: %v", invitationID, err)
		return nil, err
	}
	return &Invitation{ID: revoked.ID, Email: revoked.Email, State: string(revoked.State), ExpiresAt: revoked.ExpiresAt, Role: deref(revoked.RoleSlug)}, nil
}

// HandleLogout clears the session cookie and redirects to the WorkOS logout
// endpoint, which ends the AuthKit session server-side and then returns the
// browser to the SPA.
func (a *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	logoutURL := a.postLoginURL // fallback if we can't build the WorkOS logout URL

	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		session := workos.NewSession(a.client, c.Value, a.cookiePassword)
		if url, err := session.GetLogoutURL(r.Context(), a.postLoginURL); err == nil {
			logoutURL = url
		} else {
			log.Printf("auth: build logout URL: %v", err)
		}
	}

	a.clearSessionCookie(w)
	http.Redirect(w, r, logoutURL, http.StatusFound)
}

// WithSession is outer net/http middleware that runs before the ogen server.
// It reads the session cookie, validates (and transparently refreshes) it, and
// stashes the resulting user in the request context. ogen derives handler ctx
// from r.Context(), so Ping/GetMe can read the user via UserFromContext.
//
// Unauthenticated requests are passed through with a nil user — the JSON
// handlers decide whether that's a 401. This keeps auth enforcement in one
// place (the handlers) and cookie mechanics in another (here).
func (a *Service) WithSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := a.loadUser(w, r)
		ctx := context.WithValue(r.Context(), ctxKey{}, user)
		// Also expose the raw HTTP pair so handlers that must touch cookies
		// directly (VerifyEmail) can reach them through the ogen ctx.
		ctx = context.WithValue(ctx, httpCtxKey{}, HTTPPair{W: w, R: r})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// HTTPFromContext returns the raw HTTP writer/request stashed by WithSession.
// ok is false if they're absent (e.g. a handler invoked outside the middleware).
func HTTPFromContext(ctx context.Context) (HTTPPair, bool) {
	p, ok := ctx.Value(httpCtxKey{}).(HTTPPair)
	return p, ok
}

// loadUser unseals the session cookie and returns the user, or nil if there is
// no valid session. If the access token has expired but a refresh token is
// present, it refreshes the session and rewrites the cookie in place.
func (a *Service) loadUser(w http.ResponseWriter, r *http.Request) *SessionUser {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil
	}

	result, err := workos.AuthenticateSession(c.Value, a.cookiePassword)
	if err != nil {
		return nil
	}

	if result.Authenticated {
		return toSessionUser(result.User, result.OrganizationID, result.Role)
	}

	// Access token expired but the cookie was otherwise valid — try a refresh.
	if result.NeedsRefresh {
		refreshed, err := a.client.RefreshSession(r.Context(), c.Value, a.cookiePassword)
		if err != nil || !refreshed.Authenticated {
			// Refresh failed. This is commonly a *concurrent-refresh race*: the SPA
			// fires several requests at once, the first rotates the WorkOS refresh
			// token and rewrites the cookie, and the rest arrive carrying the now-
			// stale sealed cookie so their refresh hits invalid_grant. We must NOT
			// clear the cookie here — a sibling request may have just set a valid
			// one. Treat this single request as unauthenticated (401); the browser
			// retries and picks up the fresh cookie. Only an explicit logout clears
			// the session.
			return nil
		}
		a.setSessionCookie(w, refreshed.SealedSession)
		// Re-read the refreshed session so org_id/role come from the new token's
		// claims (RefreshSessionResult.Session doesn't expose them directly).
		if reauth, err := workos.AuthenticateSession(refreshed.SealedSession, a.cookiePassword); err == nil && reauth.Authenticated {
			return toSessionUser(reauth.User, reauth.OrganizationID, reauth.Role)
		}
	}

	return nil
}

// setSessionCookie writes the sealed session as an HttpOnly cookie. SameSite=Lax
// is enough here because the post-login redirect is a top-level navigation; the
// SPA then sends the cookie on same-site XHR with credentials: 'include'.
func (a *Service) setSessionCookie(w http.ResponseWriter, sealed string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sealed,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
	})
}

// clearSessionCookie expires the session cookie.
func (a *Service) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// UserFromContext returns the authenticated user stashed by WithSession, or nil.
func UserFromContext(ctx context.Context) *SessionUser {
	u, _ := ctx.Value(ctxKey{}).(*SessionUser)
	return u
}

// OrgUserFromRequest returns the active organization id and user id for a
// request's session (both empty if unauthenticated or org-less). It's an adapter
// for callers — like the integrations service — that need the caller's identity
// from a *http.Request without depending on the session internals.
func OrgUserFromRequest(r *http.Request) (orgID, userID string) {
	if u := UserFromContext(r.Context()); u != nil {
		return u.OrgID, u.ID
	}
	return "", ""
}

// toSessionUser flattens a WorkOS user (with *string name fields) plus the
// active organization context into our internal shape. orgID/role may be empty
// when the session isn't scoped to an organization. Returns nil for a nil user
// so callers stay simple.
func toSessionUser(u *workos.User, orgID, role string) *SessionUser {
	if u == nil {
		return nil
	}
	return &SessionUser{
		ID:                u.ID,
		Email:             u.Email,
		FirstName:         deref(u.FirstName),
		LastName:          deref(u.LastName),
		ProfilePictureURL: deref(u.ProfilePictureURL),
		OrgID:             orgID,
		Role:              role,
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
