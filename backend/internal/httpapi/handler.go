package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"xolo/backend/internal/api"
	"xolo/backend/internal/auth"
	"xolo/backend/internal/billing"
	"xolo/backend/internal/integrations"
	"xolo/backend/internal/store"
	"xolo/backend/internal/template"
)

// Handler implements the ogen-generated api.Handler interface.
// This is the only place the server's HTTP business logic lives; everything
// else (routing, decoding, encoding, validation) is generated from the spec,
// and the actual work is delegated to injected services (auth, and soon
// integrations).
//
// Auth note: the session is loaded by auth.Service's WithSession middleware and
// read here via auth.UserFromContext. ogen derives each handler's ctx from the
// HTTP request context, so the user the middleware stashed is available here.
// Handlers that must touch cookies (VerifyEmail, SelectOrg) get the raw HTTP
// pair via auth.HTTPFromContext.
type Handler struct {
	auth         *auth.Service
	integrations *integrations.Service
	billing      *billing.Service
	store        *store.Store
}

// New builds the API handler with its service dependencies.
func New(authService *auth.Service, intgService *integrations.Service, billingService *billing.Service, st *store.Store) *Handler {
	return &Handler{auth: authService, integrations: intgService, billing: billingService, store: st}
}

// invitationListLimit caps how many invitations GET /invitations returns.
const invitationListLimit = 50

// billingLockedMsg is the shared 402 body for billing-gated operations.
const billingLockedMsg = "trial expired — subscribe to keep using NotifBuddy"

// orgLocked reports whether the org's billing gates feature mutations (trial
// expired and no subscription). Fails open: a billing hiccup must not take the
// product down.
func (h Handler) orgLocked(ctx context.Context, orgID string) bool {
	if orgID == "" {
		return false
	}
	status, err := h.billing.StatusForOrg(ctx, orgID)
	if err != nil {
		return false
	}
	return status.Locked
}

// orgListLimit caps how many of a user's organizations /me returns.
const orgListLimit = 50

// Ping implements the `ping` operation: GET /ping.
// Requires an authenticated session; returns 401 otherwise.
func (Handler) Ping(ctx context.Context) (api.PingRes, error) {
	if auth.UserFromContext(ctx) == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	return &api.PongResponse{Message: "pong"}, nil
}

// GetMe implements the `getMe` operation: GET /me.
// Returns the WorkOS user, their active organization context, and the list of
// organizations they belong to. 401 when there is no valid session.
func (h Handler) GetMe(ctx context.Context) (api.GetMeRes, error) {
	user := auth.UserFromContext(ctx)
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
	p, ok := auth.HTTPFromContext(ctx)
	if !ok {
		return &api.Error{Message: "unauthorized"}, nil
	}
	user, err := h.auth.CompleteEmailVerification(p.W, p.R, req.Code)
	if err != nil {
		return &api.Error{Message: "verification failed"}, nil
	}
	return h.toUserResponse(ctx, user), nil
}

// GetPendingOrgs implements `getPendingOrgs`: GET /auth/pending-orgs.
// Returns the organizations the user may choose between during org selection.
func (h Handler) GetPendingOrgs(ctx context.Context) (api.GetPendingOrgsRes, error) {
	p, ok := auth.HTTPFromContext(ctx)
	if !ok {
		return &api.Error{Message: "no pending selection"}, nil
	}
	choices := h.auth.PendingOrgChoices(p.R)
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
	p, ok := auth.HTTPFromContext(ctx)
	if !ok {
		return &api.Error{Message: "no pending selection"}, nil
	}
	user, err := h.auth.CompleteOrgSelection(p.W, p.R, req.OrganizationId)
	if err != nil {
		return &api.Error{Message: "organization selection failed"}, nil
	}
	return h.toUserResponse(ctx, user), nil
}

// CreateOrganization implements `createOrganization`: POST /organizations.
// Creates a WorkOS org for a signed-in-but-orgless user, adds them as its
// first member, and re-scopes the session cookie to it.
func (h Handler) CreateOrganization(ctx context.Context, req *api.CreateOrganizationRequest) (api.CreateOrganizationRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.CreateOrganizationUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID != "" {
		return &api.CreateOrganizationBadRequest{Message: "session already has an organization"}, nil
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return &api.CreateOrganizationBadRequest{Message: "organization name is required"}, nil
	}
	p, ok := auth.HTTPFromContext(ctx)
	if !ok {
		return &api.CreateOrganizationUnauthorized{Message: "unauthorized"}, nil
	}
	created, err := h.auth.CreateOrganizationForUser(p.W, p.R, name)
	if err != nil {
		var userMsg auth.UserMessageError
		if errors.As(err, &userMsg) {
			return &api.CreateOrganizationBadRequest{Message: userMsg.Msg}, nil
		}
		slog.ErrorContext(ctx, "httpapi: create organization failed", "user_id", user.ID, "error", err)
		return &api.CreateOrganizationBadRequest{Message: "could not create the organization"}, nil
	}
	return h.toUserResponse(ctx, created), nil
}

// ListInvitations implements `listInvitations`: GET /invitations.
// Lists the active organization's invitations. Requires a session scoped to an
// organization.
func (h Handler) ListInvitations(ctx context.Context) (api.ListInvitationsRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.InvitationListResponse{}, nil
	}
	invites, err := h.auth.ListInvitations(ctx, user.OrgID, invitationListLimit)
	if err != nil {
		return &api.Error{Message: "failed to list invitations"}, nil
	}
	resp := &api.InvitationListResponse{}
	for _, inv := range invites {
		resp.Invitations = append(resp.Invitations, toInvitationResponse(inv))
	}
	return resp, nil
}

// memberListLimit caps how many members GET /members returns.
const memberListLimit = 200

// ListMembers implements `listMembers`: GET /members.
// Lists the active members of the caller's active organization. Requires a
// session scoped to an organization.
func (h Handler) ListMembers(ctx context.Context) (api.ListMembersRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.MemberListResponse{}, nil
	}
	members, err := h.auth.ListOrganizationMembers(ctx, user.OrgID, memberListLimit)
	if err != nil {
		return &api.Error{Message: "failed to list members"}, nil
	}
	resp := &api.MemberListResponse{}
	for _, m := range members {
		resp.Members = append(resp.Members, toMemberResponse(m))
	}
	return resp, nil
}

// UpdateMemberRole implements `updateMemberRole`: PUT /members/{membershipId}/role.
// Admin-only. Changes another member's role within the caller's active
// organization; changing your own role is rejected so an org can't lock out
// its last admin.
func (h Handler) UpdateMemberRole(ctx context.Context, req *api.UpdateMemberRoleRequest, params api.UpdateMemberRoleParams) (api.UpdateMemberRoleRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.UpdateMemberRoleUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.UpdateMemberRoleBadRequest{Message: "no active organization"}, nil
	}
	if user.Role != auth.RoleAdmin {
		return &api.UpdateMemberRoleForbidden{Message: "only admins can change member roles"}, nil
	}
	member, err := h.auth.UpdateOrganizationMemberRole(ctx, user.OrgID, user.ID, params.MembershipId, string(req.Role))
	switch {
	case errors.Is(err, auth.ErrMembershipNotFound):
		return &api.UpdateMemberRoleNotFound{Message: "membership not found"}, nil
	case errors.Is(err, auth.ErrOwnRole):
		return &api.UpdateMemberRoleBadRequest{Message: "you can't change your own role"}, nil
	case err != nil:
		return &api.UpdateMemberRoleBadRequest{Message: "failed to update the member's role"}, nil
	}
	resp := toMemberResponse(member)
	return &resp, nil
}

// CreateInvitation implements `createInvitation`: POST /invitations.
// Invites an email to the caller's active organization. Any signed-in member of
// an organization may invite (demo-simple authorization).
func (h Handler) CreateInvitation(ctx context.Context, req *api.CreateInvitationRequest) (api.CreateInvitationRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.CreateInvitationUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.CreateInvitationBadRequest{Message: "no active organization to invite to"}, nil
	}
	if h.orgLocked(ctx, user.OrgID) {
		return &api.CreateInvitationPaymentRequired{Message: billingLockedMsg}, nil
	}
	role := ""
	if r, ok := req.Role.Get(); ok {
		role = string(r)
	}
	inv, err := h.auth.SendInvitation(ctx, req.Email, user.OrgID, role, user.ID)
	if err != nil {
		return &api.CreateInvitationBadRequest{Message: "failed to send invitation"}, nil
	}
	r := toInvitationResponse(*inv)
	return &r, nil
}

// RevokeInvitation implements `revokeInvitation`: DELETE /invitations/{invitationId}.
// Revokes an invitation in the caller's active organization. Any signed-in
// member may revoke, matching invite creation (demo-simple authorization).
func (h Handler) RevokeInvitation(ctx context.Context, params api.RevokeInvitationParams) (api.RevokeInvitationRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.RevokeInvitationUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.RevokeInvitationBadRequest{Message: "no active organization"}, nil
	}
	inv, err := h.auth.RevokeInvitation(ctx, user.OrgID, params.InvitationId)
	switch {
	case errors.Is(err, auth.ErrInvitationNotFound):
		return &api.RevokeInvitationNotFound{Message: "invitation not found"}, nil
	case err != nil:
		return &api.RevokeInvitationBadRequest{Message: "failed to revoke the invitation"}, nil
	}
	r := toInvitationResponse(*inv)
	return &r, nil
}

// GetIntegrationStatus implements `getIntegrationStatus`: GET /integrations/status.
// Returns per-provider connection state for the caller's active organization.
func (h Handler) GetIntegrationStatus(ctx context.Context) (api.GetIntegrationStatusRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	statuses, err := h.integrations.Status(ctx, user.OrgID, user.ID)
	if err != nil {
		return &api.Error{Message: "failed to read integration status"}, nil
	}
	return integrationStatusResponse(h.integrations.Enabled(), statuses), nil
}

// DisconnectIntegration implements `disconnectIntegration`:
// POST /integrations/{provider}/disconnect. Removes the stored integration at
// the requested level (default workspace) and returns the refreshed status.
func (h Handler) DisconnectIntegration(ctx context.Context, params api.DisconnectIntegrationParams) (api.DisconnectIntegrationRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	level := "workspace"
	if l, ok := params.Level.Get(); ok {
		level = string(l)
	}
	if err := h.integrations.Disconnect(ctx, user.OrgID, user.ID, string(params.Provider), level); err != nil {
		return &api.Error{Message: "failed to disconnect"}, nil
	}
	statuses, err := h.integrations.Status(ctx, user.OrgID, user.ID)
	if err != nil {
		return &api.Error{Message: "failed to read integration status"}, nil
	}
	return integrationStatusResponse(h.integrations.Enabled(), statuses), nil
}

// webhookListLimit caps how many webhook events the list endpoint returns.
const webhookListLimit = 100

// ListLinearWebhooks implements `listLinearWebhooks`: GET /integrations/linear/webhooks.
// Returns the active organization's recent stored Linear webhook deliveries.
func (h Handler) ListLinearWebhooks(ctx context.Context) (api.ListLinearWebhooksRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	events, err := h.integrations.ListLinearWebhooks(ctx, user.OrgID, webhookListLimit)
	if err != nil {
		slog.ErrorContext(ctx, "httpapi: list linear webhooks failed", "org_id", user.OrgID, "error", err)
		return &api.Error{Message: "failed to list webhooks"}, nil
	}
	return webhookListResponse(events), nil
}

// GetLinearSettings implements `getLinearSettings`: GET /integrations/linear/settings.
func (h Handler) GetLinearSettings(ctx context.Context) (api.GetLinearSettingsRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.Error{Message: "unauthorized"}, nil
	}
	resp, err := h.linearSettingsResponse(ctx, user.OrgID)
	if err != nil {
		slog.ErrorContext(ctx, "httpapi: get linear settings failed", "org_id", user.OrgID, "error", err)
		return &api.Error{Message: "failed to read linear settings"}, nil
	}
	return resp, nil
}

// CreateLinearSettings implements `createLinearSettings`: POST /integrations/linear/settings.
func (h Handler) CreateLinearSettings(ctx context.Context, req *api.LinearSettings) (api.CreateLinearSettingsRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.CreateLinearSettingsUnauthorized{Message: "unauthorized"}, nil
	}
	if h.orgLocked(ctx, user.OrgID) {
		return &api.CreateLinearSettingsPaymentRequired{Message: billingLockedMsg}, nil
	}
	in := fromAPILinearSettings(req)
	in.SettingID = "" // create ignores any supplied id
	if _, err := h.integrations.SaveLinearSetting(ctx, user.OrgID, in); err != nil {
		return &api.CreateLinearSettingsBadRequest{Message: err.Error()}, nil
	}
	resp, err := h.linearSettingsResponse(ctx, user.OrgID)
	if err != nil {
		return &api.CreateLinearSettingsBadRequest{Message: "failed to read linear settings"}, nil
	}
	return resp, nil
}

// UpdateLinearSettings implements `updateLinearSettings`: PUT /integrations/linear/settings/{settingId}.
func (h Handler) UpdateLinearSettings(ctx context.Context, req *api.LinearSettings, params api.UpdateLinearSettingsParams) (api.UpdateLinearSettingsRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.UpdateLinearSettingsUnauthorized{Message: "unauthorized"}, nil
	}
	if h.orgLocked(ctx, user.OrgID) {
		return &api.UpdateLinearSettingsPaymentRequired{Message: billingLockedMsg}, nil
	}
	in := fromAPILinearSettings(req)
	in.SettingID = params.SettingId // the URL is authoritative for which config to update
	if _, err := h.integrations.SaveLinearSetting(ctx, user.OrgID, in); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return &api.UpdateLinearSettingsNotFound{Message: "no such config"}, nil
		}
		return &api.UpdateLinearSettingsBadRequest{Message: err.Error()}, nil
	}
	resp, err := h.linearSettingsResponse(ctx, user.OrgID)
	if err != nil {
		return &api.UpdateLinearSettingsBadRequest{Message: "failed to read linear settings"}, nil
	}
	return resp, nil
}

// DeleteLinearSettings implements `deleteLinearSettings`: DELETE /integrations/linear/settings/{settingId}.
func (h Handler) DeleteLinearSettings(ctx context.Context, params api.DeleteLinearSettingsParams) (api.DeleteLinearSettingsRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.DeleteLinearSettingsUnauthorized{Message: "unauthorized"}, nil
	}
	if h.orgLocked(ctx, user.OrgID) {
		return &api.DeleteLinearSettingsPaymentRequired{Message: billingLockedMsg}, nil
	}
	if err := h.integrations.DeleteLinearSetting(ctx, user.OrgID, params.SettingId); err != nil {
		slog.ErrorContext(ctx, "httpapi: delete linear setting failed", "setting_id", params.SettingId, "org_id", user.OrgID, "error", err)
		return &api.DeleteLinearSettingsUnauthorized{Message: "failed to delete config"}, nil
	}
	resp, err := h.linearSettingsResponse(ctx, user.OrgID)
	if err != nil {
		return &api.DeleteLinearSettingsUnauthorized{Message: "failed to read linear settings"}, nil
	}
	return resp, nil
}

// SyncSettings implements `syncSettings`: POST /integrations/settings/sync.
// It re-syncs Linear team states and Slack members in parallel, then returns the
// refreshed settings. Each sync is non-fatal (logged) so a failure in one still
// returns whatever is stored.
func (h Handler) SyncSettings(ctx context.Context) (api.SyncSettingsRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.SyncSettingsUnauthorized{Message: "unauthorized"}, nil
	}
	if h.orgLocked(ctx, user.OrgID) {
		return &api.SyncSettingsPaymentRequired{Message: billingLockedMsg}, nil
	}
	orgID := user.OrgID
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := h.integrations.SyncLinearTeamStates(ctx, orgID); err != nil {
			slog.ErrorContext(ctx, "httpapi: sync linear team states failed", "org_id", orgID, "error", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := h.integrations.SyncSlackMembers(ctx, orgID); err != nil {
			slog.ErrorContext(ctx, "httpapi: sync slack members failed", "org_id", orgID, "error", err)
		}
	}()
	wg.Wait()

	resp, err := h.linearSettingsResponse(ctx, orgID)
	if err != nil {
		return &api.SyncSettingsUnauthorized{Message: "failed to read linear settings"}, nil
	}
	return resp, nil
}

// TestLinearTemplate implements `testLinearTemplate`: POST /integrations/linear/settings/test.
func (h Handler) TestLinearTemplate(ctx context.Context, req *api.TemplateTestRequest) (api.TestLinearTemplateRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.TestLinearTemplateUnauthorized{Message: "unauthorized"}, nil
	}
	if h.orgLocked(ctx, user.OrgID) {
		return &api.TestLinearTemplatePaymentRequired{Message: billingLockedMsg}, nil
	}
	// Resolve the event: a built-in sample id or a pasted raw JSON envelope.
	var rawEvent []byte
	if id, ok := req.SampleId.Get(); ok && id != "" {
		raw, err := h.integrations.LinearSampleEventRaw(id)
		if err != nil {
			return &api.TestLinearTemplateBadRequest{Message: "unknown sample event"}, nil
		}
		rawEvent = raw
	} else if ev, ok := req.Event.Get(); ok && ev != "" {
		rawEvent = []byte(ev)
	} else {
		return &api.TestLinearTemplateBadRequest{Message: "provide a sampleId or an event"}, nil
	}

	evt, err := template.ParseEvent(rawEvent)
	if err != nil {
		return &api.TestLinearTemplateBadRequest{Message: "invalid event JSON"}, nil
	}
	res := h.integrations.TestLinearTemplate(evt, integrations.LinearSettings{
		NameTemplate:         req.NameTemplate.Or(""),
		CreationMode:         req.CreationMode.Or("manual"),
		TriggerStatus:        req.TriggerStatus.Or(""),
		ConditionExpr:        req.Condition.Or(""),
		ArchiveMode:          req.ArchiveMode.Or("manual"),
		ArchiveStatus:        req.ArchiveStatus.Or(""),
		ArchiveConditionExpr: req.ArchiveCondition.Or(""),
	})
	out := &api.TemplateTestResponse{
		Name:         res.Name,
		WouldCreate:  res.WouldCreate,
		WouldArchive: res.WouldArchive,
	}
	if res.Err != "" {
		out.Error = api.NewOptString(res.Err)
	}
	return out, nil
}

// linearSettingsResponse builds the shared response for every settings endpoint:
// all of the org's configs, the connected flag, the synced teams (for the team
// picker + status dropdown), and the sample events.
func (h Handler) linearSettingsResponse(ctx context.Context, orgID string) (*api.LinearSettingsResponse, error) {
	configs, err := h.integrations.ListLinearSettings(ctx, orgID)
	if err != nil {
		return nil, err
	}
	teams, err := h.integrations.GetLinearTeamStates(ctx, orgID)
	if err != nil {
		return nil, err
	}
	members, err := h.integrations.GetSlackMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	samples, err := h.integrations.ListLinearSampleEvents()
	if err != nil {
		return nil, err
	}
	resp := &api.LinearSettingsResponse{
		// "Connected" here means the org is ready to run the sync — both Linear
		// and Slack connected at the workspace level, since the rules create
		// Slack channels from Linear issues.
		Connected:    h.integrations.LinearSyncReady(ctx, orgID),
		Configs:      make([]api.LinearSettings, 0, len(configs)),
		Teams:        make([]api.LinearTeamState, 0, len(teams)),
		SlackMembers: make([]api.SlackMember, 0, len(members)),
	}
	for _, c := range configs {
		resp.Configs = append(resp.Configs, toAPILinearSettings(c))
	}
	for _, t := range teams {
		resp.Teams = append(resp.Teams, toAPILinearTeamState(t))
	}
	for _, m := range members {
		resp.SlackMembers = append(resp.SlackMembers, toAPISlackMember(m))
	}
	for _, s := range samples {
		resp.SampleEvents = append(resp.SampleEvents, api.SampleEvent{ID: s.ID, Label: s.Label, Raw: s.Raw})
	}
	return resp, nil
}

// fromAPILinearSettings maps a request DTO to the service config.
func fromAPILinearSettings(req *api.LinearSettings) integrations.LinearSettings {
	return integrations.LinearSettings{
		SettingID:            req.SettingId.Or(""),
		TeamID:               req.TeamId,
		CreationMode:         string(req.CreationMode),
		TriggerStatus:        req.TriggerStatus.Or(""),
		NameTemplate:         req.NameTemplate.Or(""),
		ConditionExpr:        req.ConditionExpr.Or(""),
		ArchiveMode:          string(req.ArchiveMode.Or("manual")),
		ArchiveStatus:        req.ArchiveStatus.Or(""),
		ArchiveConditionExpr: req.ArchiveConditionExpr.Or(""),
		AutoAddMembers:       orEmptyStrings(req.AutoAddMembers),
	}
}

// toAPILinearSettings maps a service config to the generated type.
func toAPILinearSettings(s integrations.LinearSettings) api.LinearSettings {
	out := api.LinearSettings{
		CreationMode:   api.LinearSettingsCreationMode(s.CreationMode),
		AutoAddMembers: orEmptyStrings(s.AutoAddMembers),
		TeamId:         s.TeamID,
	}
	if s.SettingID != "" {
		out.SettingId = api.NewOptString(s.SettingID)
	}
	if s.TriggerStatus != "" {
		out.TriggerStatus = api.NewOptString(s.TriggerStatus)
	}
	if s.NameTemplate != "" {
		out.NameTemplate = api.NewOptString(s.NameTemplate)
	}
	if s.ConditionExpr != "" {
		out.ConditionExpr = api.NewOptString(s.ConditionExpr)
	}
	if s.ArchiveMode != "" {
		out.ArchiveMode = api.NewOptLinearSettingsArchiveMode(api.LinearSettingsArchiveMode(s.ArchiveMode))
	}
	if s.ArchiveStatus != "" {
		out.ArchiveStatus = api.NewOptString(s.ArchiveStatus)
	}
	if s.ArchiveConditionExpr != "" {
		out.ArchiveConditionExpr = api.NewOptString(s.ArchiveConditionExpr)
	}
	return out
}

// toAPISlackMember maps a synced Slack member to the generated type.
func toAPISlackMember(m integrations.SlackMemberView) api.SlackMember {
	out := api.SlackMember{MemberId: m.MemberID, Name: m.Name, IsBot: m.IsBot}
	if m.IconURL != "" {
		out.IconUrl = api.NewOptString(m.IconURL)
	}
	return out
}

// orEmptyStrings returns a non-nil empty slice for nil input.
func orEmptyStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// toAPILinearTeamState maps a synced team + its states to the generated type.
func toAPILinearTeamState(t integrations.LinearTeamStatesView) api.LinearTeamState {
	out := api.LinearTeamState{
		TeamId:   t.TeamID,
		TeamName: t.TeamName,
		States:   make([]api.LinearWorkflowState, 0, len(t.States)),
	}
	if t.TeamKey != "" {
		out.TeamKey = api.NewOptString(t.TeamKey)
	}
	for _, st := range t.States {
		ws := api.LinearWorkflowState{ID: st.ID, Name: st.Name, Type: st.Type}
		if st.Color != "" {
			ws.Color = api.NewOptString(st.Color)
		}
		out.States = append(out.States, ws)
	}
	return out
}

// webhookListResponse maps a service webhook-event slice to the generated type.
func webhookListResponse(events []integrations.WebhookEvent) *api.WebhookListResponse {
	resp := &api.WebhookListResponse{}
	for _, e := range events {
		item := api.WebhookEvent{DeliveryId: e.DeliveryID, EventType: e.EventType, ReceivedAt: e.ReceivedAt}
		if e.Action != "" {
			item.Action = api.NewOptString(e.Action)
		}
		if len(e.Payload) > 0 {
			item.Payload = api.NewOptString(string(e.Payload))
		}
		resp.Events = append(resp.Events, item)
	}
	return resp
}

// integrationStatusResponse maps the service's status slice to the generated type.
func integrationStatusResponse(configured bool, statuses []integrations.ProviderStatus) *api.IntegrationStatusResponse {
	resp := &api.IntegrationStatusResponse{Configured: configured}
	for _, st := range statuses {
		item := api.IntegrationStatus{
			Provider:  st.Provider,
			Level:     api.IntegrationStatusLevel(st.Level),
			Connected: st.Connected,
		}
		if st.Account != "" {
			item.Account = api.NewOptString(st.Account)
		}
		if st.ConnectedBy != "" {
			item.ConnectedBy = api.NewOptString(st.ConnectedBy)
		}
		resp.Integrations = append(resp.Integrations, item)
	}
	return resp
}

// toUserResponse maps our internal auth.SessionUser to the generated UserResponse,
// including the active org/role and the list of organizations the user belongs
// to (fetched best-effort).
func (h Handler) toUserResponse(ctx context.Context, user *auth.SessionUser) *api.UserResponse {
	resp := &api.UserResponse{ID: user.ID, Email: user.Email}
	if user.FirstName != "" {
		resp.FirstName = api.NewOptString(user.FirstName)
	}
	if user.LastName != "" {
		resp.LastName = api.NewOptString(user.LastName)
	}
	if user.ProfilePictureURL != "" {
		resp.ProfilePictureUrl = api.NewOptString(user.ProfilePictureURL)
	}
	if user.OrgID != "" {
		resp.OrganizationId = api.NewOptString(user.OrgID)
	}
	if user.Role != "" {
		resp.Role = api.NewOptString(user.Role)
	}
	for _, m := range h.auth.ListUserOrganizations(ctx, user.ID, orgListLimit) {
		org := api.Organization{ID: m.ID, Name: m.Name}
		if m.Role != "" {
			org.Role = api.NewOptString(m.Role)
		}
		resp.Organizations = append(resp.Organizations, org)
	}
	// Embed the active org's billing summary so the SPA can show the trial
	// banner / lock screen without a second request. Best-effort: /me must
	// keep working when billing is unconfigured (no database). This is also
	// what lazily starts a fresh org's 21-day trial.
	if user.OrgID != "" {
		if status, err := h.billing.StatusForOrg(ctx, user.OrgID); err == nil {
			resp.Billing = api.NewOptBillingSummary(api.BillingSummary{
				Plan:        api.BillingSummaryPlan(status.Plan),
				Locked:      status.Locked,
				TrialEndsAt: status.TrialEndsAt,
			})
		}
	}
	return resp
}

// toInvitationResponse maps our internal invitation to the generated type.
func toInvitationResponse(inv auth.Invitation) api.InvitationResponse {
	r := api.InvitationResponse{ID: inv.ID, Email: inv.Email, State: inv.State}
	if inv.ExpiresAt != "" {
		r.ExpiresAt = api.NewOptString(inv.ExpiresAt)
	}
	if inv.Role != "" {
		r.Role = api.NewOptString(inv.Role)
	}
	return r
}

func toMemberResponse(m auth.Member) api.MemberResponse {
	r := api.MemberResponse{ID: m.ID, UserId: m.UserID, Email: m.Email}
	if m.FirstName != "" {
		r.FirstName = api.NewOptString(m.FirstName)
	}
	if m.LastName != "" {
		r.LastName = api.NewOptString(m.LastName)
	}
	if m.Role != "" {
		r.Role = api.NewOptString(m.Role)
	}
	if m.ProfilePictureURL != "" {
		r.ProfilePictureUrl = api.NewOptString(m.ProfilePictureURL)
	}
	return r
}
