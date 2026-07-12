package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"xolo/backend/internal/api"
	"xolo/backend/internal/auth"
	"xolo/backend/internal/billing"
)

// billingAdminOnlyMsg is the 403 body for billing actions gated to admins.
const billingAdminOnlyMsg = "only admins can manage billing"

// GetBilling implements `getBilling`: GET /billing.
// Returns the active organization's billing status, lazily starting its
// 21-day trial on first touch, and trues the Stripe seat quantity up while a
// subscription is live (the billing page is one of the seat-sync paths).
func (h Handler) GetBilling(ctx context.Context) (api.GetBillingRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.GetBillingUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.GetBillingBadRequest{Message: "no active organization"}, nil
	}
	h.billing.ReconcileSeats(ctx, user.OrgID)
	status, err := h.billing.StatusForOrg(ctx, user.OrgID)
	if err != nil {
		slog.ErrorContext(ctx, "httpapi: billing status failed", "org_id", user.OrgID, "error", err)
		return &api.GetBillingBadRequest{Message: "billing is not configured"}, nil
	}
	return toBillingStatusResponse(status), nil
}

// CreateBillingCheckout implements `createBillingCheckout`: POST /billing/checkout.
// Starts a subscription-mode Stripe Checkout session for the Pro plan.
func (h Handler) CreateBillingCheckout(ctx context.Context) (api.CreateBillingCheckoutRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.CreateBillingCheckoutUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.CreateBillingCheckoutBadRequest{Message: "no active organization"}, nil
	}
	if user.Role != auth.RoleAdmin {
		return &api.CreateBillingCheckoutForbidden{Message: billingAdminOnlyMsg}, nil
	}
	url, err := h.billing.CreateCheckout(ctx, user.OrgID, user.Email)
	switch {
	case errors.Is(err, billing.ErrDisabled):
		return &api.CreateBillingCheckoutBadRequest{Message: "billing is disabled while NotifBuddy is in beta"}, nil
	case errors.Is(err, billing.ErrAlreadySubscribed):
		return &api.CreateBillingCheckoutConflict{Message: "already subscribed"}, nil
	case errors.Is(err, billing.ErrNotConfigured):
		return &api.CreateBillingCheckoutBadRequest{Message: "billing is not configured"}, nil
	case err != nil:
		slog.ErrorContext(ctx, "httpapi: create checkout failed", "org_id", user.OrgID, "error", err)
		return &api.CreateBillingCheckoutBadRequest{Message: "failed to start checkout"}, nil
	}
	return &api.BillingRedirectResponse{URL: url}, nil
}

// CreateBillingPortal implements `createBillingPortal`: POST /billing/portal.
// Opens the Stripe Customer Portal for the org's customer.
func (h Handler) CreateBillingPortal(ctx context.Context) (api.CreateBillingPortalRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.CreateBillingPortalUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.CreateBillingPortalBadRequest{Message: "no active organization"}, nil
	}
	if user.Role != auth.RoleAdmin {
		return &api.CreateBillingPortalForbidden{Message: billingAdminOnlyMsg}, nil
	}
	url, err := h.billing.CreatePortal(ctx, user.OrgID)
	switch {
	case errors.Is(err, billing.ErrDisabled):
		return &api.CreateBillingPortalBadRequest{Message: "billing is disabled while NotifBuddy is in beta"}, nil
	case errors.Is(err, billing.ErrNoCustomer):
		return &api.CreateBillingPortalBadRequest{Message: "no billing account yet — subscribe first"}, nil
	case errors.Is(err, billing.ErrNotConfigured):
		return &api.CreateBillingPortalBadRequest{Message: "billing is not configured"}, nil
	case err != nil:
		slog.ErrorContext(ctx, "httpapi: create portal failed", "org_id", user.OrgID, "error", err)
		return &api.CreateBillingPortalBadRequest{Message: "failed to open the billing portal"}, nil
	}
	return &api.BillingRedirectResponse{URL: url}, nil
}

// SubmitOssApplication implements `submitOssApplication`: POST /billing/oss-application.
// Records a free open-source tier application (manually reviewed).
func (h Handler) SubmitOssApplication(ctx context.Context, req *api.OssApplicationRequest) (api.SubmitOssApplicationRes, error) {
	user := auth.UserFromContext(ctx)
	if user == nil {
		return &api.SubmitOssApplicationUnauthorized{Message: "unauthorized"}, nil
	}
	if user.OrgID == "" {
		return &api.SubmitOssApplicationBadRequest{Message: "no active organization"}, nil
	}
	if user.Role != auth.RoleAdmin {
		return &api.SubmitOssApplicationForbidden{Message: billingAdminOnlyMsg}, nil
	}
	sponsorURL := strings.TrimSpace(req.SponsorUrl)
	if !strings.HasPrefix(sponsorURL, "https://") && !strings.HasPrefix(sponsorURL, "http://") {
		return &api.SubmitOssApplicationBadRequest{Message: "sponsorUrl must be a http(s) URL"}, nil
	}
	note := ""
	if n, ok := req.Note.Get(); ok {
		note = strings.TrimSpace(n)
	}
	status, err := h.billing.SubmitOSSApplication(ctx, user.OrgID, sponsorURL, note)
	switch {
	case errors.Is(err, billing.ErrDisabled):
		return &api.SubmitOssApplicationBadRequest{Message: "billing is disabled while NotifBuddy is in beta"}, nil
	case errors.Is(err, billing.ErrAlreadySubscribed):
		return &api.SubmitOssApplicationConflict{Message: "already approved or subscribed"}, nil
	case err != nil:
		slog.ErrorContext(ctx, "httpapi: oss application failed", "org_id", user.OrgID, "error", err)
		return &api.SubmitOssApplicationBadRequest{Message: "failed to record the application"}, nil
	}
	return toBillingStatusResponse(status), nil
}

// toBillingStatusResponse maps a derived billing status to the generated type.
func toBillingStatusResponse(st billing.Status) *api.BillingStatusResponse {
	resp := &api.BillingStatusResponse{
		Plan:              api.BillingStatusResponsePlan(st.Plan),
		Locked:            st.Locked,
		TrialEndsAt:       st.TrialEndsAt,
		Subscribed:        st.Subscribed,
		PriceCentsPerSeat: billing.PriceCentsPerSeat,
	}
	if st.StripeStatus != "" {
		resp.StripeStatus = api.NewOptString(st.StripeStatus)
	}
	if st.Seats > 0 {
		resp.Seats = api.NewOptInt(st.Seats)
	}
	if st.OSSApplicationStatus != "" {
		resp.OssApplicationStatus = api.NewOptBillingStatusResponseOssApplicationStatus(
			api.BillingStatusResponseOssApplicationStatus(st.OSSApplicationStatus))
	}
	return resp
}
