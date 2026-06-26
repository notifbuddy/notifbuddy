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
// Handlers that must touch cookies (VerifyEmail, SelectOrg) get the raw HTTP
// pair via httpFromContext.
type Handler struct {
	auth *authConfig
}

// invitationListLimit caps how many invitations GET /invitations returns.
const invitationListLimit = 50

// orgListLimit caps how many of a user's organizations /me returns.
const orgListLimit = 50

// Ping implements the `ping` operation: GET /ping.
// Requires an authenticated session; returns 401 otherwise.
func (Handler) Ping(ctx context.Context) (api.PingRes, error) {
	if userFromContext(ctx) == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	return &api.PongResponse{Message: "pong"}, nil
}

// GetMe implements the `getMe` operation: GET /me.
// Returns the WorkOS user, their active organization context, and the list of
// organizations they belong to. 401 when there is no valid session.
func (h Handler) GetMe(ctx context.Context) (api.GetMeRes, error) {
	user := userFromContext(ctx)
	if user == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	resp := h.toUserResponse(ctx, user)
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
	return h.toUserResponse(ctx, user), nil
}

// GetPendingOrgs implements `getPendingOrgs`: GET /auth/pending-orgs.
// Returns the organizations the user may choose between during org selection.
func (h Handler) GetPendingOrgs(ctx context.Context) (api.GetPendingOrgsRes, error) {
	p, ok := httpFromContext(ctx)
	if !ok {
		return &api.Error{Message: "no pending selection"}, nil
	}
	choices := h.auth.pendingOrgChoices(p.r)
	if len(choices) == 0 {
		return &api.Error{Message: "no pending selection"}, nil
	}
	resp := &api.PendingOrganizations{}
	for _, c := range choices {
		resp.Organizations = append(resp.Organizations, api.Organization{ID: c.ID, Name: c.Name})
	}
	return resp, nil
}

// SelectOrg implements `selectOrg`: POST /auth/select-org.
// Completes a login gated on organization selection.
func (h Handler) SelectOrg(ctx context.Context, req *api.SelectOrgRequest) (api.SelectOrgRes, error) {
	p, ok := httpFromContext(ctx)
	if !ok {
		return &api.Error{Message: "no pending selection"}, nil
	}
	user, err := h.auth.completeOrgSelection(p.w, p.r, req.OrganizationId)
	if err != nil {
		return &api.Error{Message: "organization selection failed"}, nil
	}
	return h.toUserResponse(ctx, user), nil
}

// ListInvitations implements `listInvitations`: GET /invitations.
// Lists the active organization's invitations. Requires a session scoped to an
// organization.
func (h Handler) ListInvitations(ctx context.Context) (api.ListInvitationsRes, error) {
	user := userFromContext(ctx)
	if user == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.InvitationListResponse{}, nil
	}
	invites, err := h.auth.listInvitations(ctx, user.OrgID, invitationListLimit)
	if err != nil {
		return &api.Error{Message: "failed to list invitations"}, nil
	}
	resp := &api.InvitationListResponse{}
	for _, inv := range invites {
		resp.Invitations = append(resp.Invitations, toInvitationResponse(inv))
	}
	return resp, nil
}

// CreateInvitation implements `createInvitation`: POST /invitations.
// Invites an email to the caller's active organization. Any signed-in member of
// an organization may invite (demo-simple authorization).
func (h Handler) CreateInvitation(ctx context.Context, req *api.CreateInvitationRequest) (api.CreateInvitationRes, error) {
	user := userFromContext(ctx)
	if user == nil {
		return &api.CreateInvitationUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.CreateInvitationBadRequest{Message: "no active organization to invite to"}, nil
	}
	role := ""
	if r, ok := req.Role.Get(); ok {
		role = r
	}
	inv, err := h.auth.sendInvitation(ctx, req.Email, user.OrgID, role, user.ID)
	if err != nil {
		return &api.CreateInvitationBadRequest{Message: "failed to send invitation"}, nil
	}
	r := toInvitationResponse(*inv)
	return &r, nil
}

// toUserResponse maps our internal sessionUser to the generated UserResponse,
// including the active org/role and the list of organizations the user belongs
// to (fetched best-effort).
func (h Handler) toUserResponse(ctx context.Context, user *sessionUser) *api.UserResponse {
	resp := &api.UserResponse{ID: user.ID, Email: user.Email}
	if user.FirstName != "" {
		resp.FirstName = api.NewOptString(user.FirstName)
	}
	if user.LastName != "" {
		resp.LastName = api.NewOptString(user.LastName)
	}
	if user.OrgID != "" {
		resp.OrganizationId = api.NewOptString(user.OrgID)
	}
	if user.Role != "" {
		resp.Role = api.NewOptString(user.Role)
	}
	for _, m := range h.auth.listUserOrganizations(ctx, user.ID, orgListLimit) {
		org := api.Organization{ID: m.ID, Name: m.Name}
		if m.Role != "" {
			org.Role = api.NewOptString(m.Role)
		}
		resp.Organizations = append(resp.Organizations, org)
	}
	return resp
}

// toInvitationResponse maps our internal invitation to the generated type.
func toInvitationResponse(inv invitation) api.InvitationResponse {
	r := api.InvitationResponse{ID: inv.ID, Email: inv.Email, State: inv.State}
	if inv.ExpiresAt != "" {
		r.ExpiresAt = api.NewOptString(inv.ExpiresAt)
	}
	return r
}
