package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	workos "github.com/workos/workos-go/v9"
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

// authConfig holds everything the auth flow needs, resolved once at startup.
type authConfig struct {
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
// ogen only hands handlers a context — so withSession stashes the HTTP objects
// here for those handlers to retrieve via httpFromContext.
type httpCtxKey struct{}

// httpPair bundles the writer and request for handlers that need raw HTTP access.
type httpPair struct {
	w http.ResponseWriter
	r *http.Request
}

// sessionUser is the minimal user info the JSON handlers need. It is nil in the
// context when the request is unauthenticated.
type sessionUser struct {
	ID        string
	Email     string
	FirstName string
	LastName  string
}

// newAuthConfig builds the WorkOS client from the loaded Config. Secret fields
// (API key, cookie password) have already been resolved from their env
// references during config load.
func newAuthConfig(cfg Config) *authConfig {
	return &authConfig{
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

// handleLogin redirects the browser to the WorkOS AuthKit hosted login page.
// AuthKit owns the login screen (email/password, social, SSO — configured in
// the WorkOS dashboard); we only kick off the flow.
func (a *authConfig) handleLogin(w http.ResponseWriter, r *http.Request) {
	params := workos.AuthKitAuthorizationURLParams{
		RedirectURI: a.redirectURI,
	}
	// Send the user straight to a specific provider (e.g. GitHub) when
	// configured, instead of AuthKit's selector.
	if a.loginProvider != "" {
		params.Provider = &a.loginProvider
	}
	url, err := a.client.GetAuthKitAuthorizationURL(params)
	if err != nil {
		log.Printf("auth: build authorization URL: %v", err)
		http.Error(w, "failed to start login", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

// handleCallback is the WORKOS_REDIRECT_URI target. AuthKit redirects here with
// a `code` query param after the user signs in. We exchange it for tokens, seal
// them into the session cookie, and bounce the browser back to the SPA.
func (a *authConfig) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		// AuthKit returns error/error_description on failure (e.g. user cancelled).
		if e := r.URL.Query().Get("error"); e != "" {
			log.Printf("auth: callback error: %s — %s", e, r.URL.Query().Get("error_description"))
		}
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	resp, err := a.client.UserManagement().AuthenticateWithCode(r.Context(),
		&workos.UserManagementAuthenticateWithCodeParams{Code: code})
	if err != nil {
		// email_verification_required is an expected branch (notably for GitHub
		// OAuth, whose users land unverified): WorkOS emails a code and returns a
		// pending_authentication_token. Stash that token and send the user to the
		// SPA's verification step rather than failing.
		var apiErr *workos.APIError
		if errors.As(err, &apiErr) && apiErr.Code == "email_verification_required" {
			a.startEmailVerification(w, r, apiErr.PendingAuthenticationToken)
			return
		}
		// Any other error: log the structured details and fail.
		if apiErr != nil {
			log.Printf("auth: exchange code failed: status=%d code=%q message=%q error=%q error_description=%q request_id=%q body=%s",
				apiErr.StatusCode, apiErr.Code, apiErr.Message, apiErr.ErrorCode, apiErr.ErrorDescription, apiErr.RequestID, apiErr.RawBody)
		} else {
			log.Printf("auth: exchange code failed: %v", err)
		}
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	if err := a.establishSession(w, resp); err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, a.postLoginURL, http.StatusFound)
}

// startEmailVerification seals the pending authentication token into a
// short-lived cookie and redirects the browser to the SPA with ?verify=1 so it
// shows the code-entry step. WorkOS has already emailed the code.
func (a *authConfig) startEmailVerification(w http.ResponseWriter, r *http.Request, pendingToken string) {
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
// cookie. Shared by the OAuth callback and the email-verification completion.
// It only sets the cookie and returns an error; the caller decides how to
// report failure (redirect vs. JSON), so this never writes to w on error.
func (a *authConfig) establishSession(w http.ResponseWriter, resp *workos.AuthenticateResponse) error {
	sealed, err := workos.SealSessionFromAuthResponse(
		resp.AccessToken, resp.RefreshToken, resp.User, resp.Impersonator, a.cookiePassword)
	if err != nil {
		log.Printf("auth: seal session: %v", err)
		return err
	}
	a.setSessionCookie(w, sealed)
	return nil
}

// completeEmailVerification finishes a login that was gated on email
// verification. It reads the sealed pending token from the request cookie,
// exchanges it plus the user-supplied code for a session, sets the session
// cookie, clears the pending cookie, and returns the authenticated user.
// Returns nil + an error on any failure (bad/expired code, missing pending
// cookie) — the caller maps that to a 401.
func (a *authConfig) completeEmailVerification(w http.ResponseWriter, r *http.Request, code string) (*sessionUser, error) {
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

	if err := a.establishSession(w, resp); err != nil {
		return nil, err
	}
	a.clearPendingCookie(w)
	return toSessionUser(resp.User), nil
}

// clearPendingCookie expires the pending-verification cookie.
func (a *authConfig) clearPendingCookie(w http.ResponseWriter) {
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

// handleLogout clears the session cookie and redirects to the WorkOS logout
// endpoint, which ends the AuthKit session server-side and then returns the
// browser to the SPA.
func (a *authConfig) handleLogout(w http.ResponseWriter, r *http.Request) {
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

// withSession is outer net/http middleware that runs before the ogen server.
// It reads the session cookie, validates (and transparently refreshes) it, and
// stashes the resulting user in the request context. ogen derives handler ctx
// from r.Context(), so Ping/GetMe can read the user via userFromContext.
//
// Unauthenticated requests are passed through with a nil user — the JSON
// handlers decide whether that's a 401. This keeps auth enforcement in one
// place (the handlers) and cookie mechanics in another (here).
func (a *authConfig) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := a.loadUser(w, r)
		ctx := context.WithValue(r.Context(), ctxKey{}, user)
		// Also expose the raw HTTP pair so handlers that must touch cookies
		// directly (VerifyEmail) can reach them through the ogen ctx.
		ctx = context.WithValue(ctx, httpCtxKey{}, httpPair{w: w, r: r})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// httpFromContext returns the raw HTTP writer/request stashed by withSession.
// ok is false if they're absent (e.g. a handler invoked outside the middleware).
func httpFromContext(ctx context.Context) (httpPair, bool) {
	p, ok := ctx.Value(httpCtxKey{}).(httpPair)
	return p, ok
}

// loadUser unseals the session cookie and returns the user, or nil if there is
// no valid session. If the access token has expired but a refresh token is
// present, it refreshes the session and rewrites the cookie in place.
func (a *authConfig) loadUser(w http.ResponseWriter, r *http.Request) *sessionUser {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return nil
	}

	result, err := workos.AuthenticateSession(c.Value, a.cookiePassword)
	if err != nil {
		return nil
	}

	if result.Authenticated {
		return toSessionUser(result.User)
	}

	// Access token expired but the cookie was otherwise valid — try a refresh.
	if result.NeedsRefresh {
		refreshed, err := a.client.RefreshSession(r.Context(), c.Value, a.cookiePassword)
		if err != nil || !refreshed.Authenticated {
			a.clearSessionCookie(w)
			return nil
		}
		a.setSessionCookie(w, refreshed.SealedSession)
		if refreshed.Session != nil {
			return toSessionUser(refreshed.Session.User)
		}
	}

	return nil
}

// setSessionCookie writes the sealed session as an HttpOnly cookie. SameSite=Lax
// is enough here because the post-login redirect is a top-level navigation; the
// SPA then sends the cookie on same-site XHR with credentials: 'include'.
func (a *authConfig) setSessionCookie(w http.ResponseWriter, sealed string) {
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
func (a *authConfig) clearSessionCookie(w http.ResponseWriter) {
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

// userFromContext returns the authenticated user stashed by withSession, or nil.
func userFromContext(ctx context.Context) *sessionUser {
	u, _ := ctx.Value(ctxKey{}).(*sessionUser)
	return u
}

// toSessionUser flattens a WorkOS user (with *string name fields) into our
// internal shape. Returns nil for a nil input so callers stay simple.
func toSessionUser(u *workos.User) *sessionUser {
	if u == nil {
		return nil
	}
	return &sessionUser{
		ID:        u.ID,
		Email:     u.Email,
		FirstName: deref(u.FirstName),
		LastName:  deref(u.LastName),
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
