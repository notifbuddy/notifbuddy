package main

import (
	"context"

	"xolo/backend/internal/api"
)

// Handler implements the ogen-generated api.Handler interface.
// This is the only place the server's business logic lives; everything
// else (routing, decoding, encoding, validation) is generated from the spec.
//
// Auth note: the session is loaded by the outer withSession middleware (auth.go)
// and read here via userFromContext. ogen derives each handler's ctx from the
// HTTP request context, so the user the middleware stashed is available here.
type Handler struct {
	auth *authConfig
}

// Ping implements the `ping` operation: GET /ping.
// Requires an authenticated session; returns 401 otherwise.
func (Handler) Ping(ctx context.Context) (api.PingRes, error) {
	if userFromContext(ctx) == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	return &api.PongResponse{Message: "pong"}, nil
}

// GetMe implements the `getMe` operation: GET /me.
// Returns the WorkOS user backing the current session, or 401 when there is no
// valid session.
func (Handler) GetMe(ctx context.Context) (api.GetMeRes, error) {
	user := userFromContext(ctx)
	if user == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	resp := &api.UserResponse{
		ID:    user.ID,
		Email: user.Email,
	}
	if user.FirstName != "" {
		resp.FirstName = api.NewOptString(user.FirstName)
	}
	if user.LastName != "" {
		resp.LastName = api.NewOptString(user.LastName)
	}
	return resp, nil
}

// VerifyEmail implements the `verifyEmail` operation: POST /auth/verify-email.
// It completes a login that WorkOS gated on email verification (see
// startEmailVerification in auth.go) by exchanging the user-entered code plus
// the stashed pending token for a session. On success the session cookie is set
// and the user is returned; on any failure it returns 401.
func (h Handler) VerifyEmail(ctx context.Context, req *api.VerifyEmailRequest) (api.VerifyEmailRes, error) {
	p, ok := httpFromContext(ctx)
	if !ok {
		return &api.Error{Message: "unauthorized"}, nil
	}
	user, err := h.auth.completeEmailVerification(p.w, p.r, req.Code)
	if err != nil {
		return &api.Error{Message: "verification failed"}, nil
	}
	resp := &api.UserResponse{ID: user.ID, Email: user.Email}
	if user.FirstName != "" {
		resp.FirstName = api.NewOptString(user.FirstName)
	}
	if user.LastName != "" {
		resp.LastName = api.NewOptString(user.LastName)
	}
	return resp, nil
}
