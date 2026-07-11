package billing

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/stripe/stripe-go/v86"
)

// Sentinel errors the HTTP layer maps to status codes.
var (
	// ErrAlreadySubscribed reports a checkout attempt while a live
	// subscription exists (409).
	ErrAlreadySubscribed = fmt.Errorf("billing: already subscribed")
	// ErrNoCustomer reports a portal request before any checkout created the
	// org's Stripe customer (400).
	ErrNoCustomer = fmt.Errorf("billing: no stripe customer yet")
)

// stripeClient lazily builds the Stripe client; nil when no API key is set.
func (s *Service) stripeClient() *stripe.Client {
	if s.cfg.Stripe.APIKey == "" {
		return nil
	}
	if s.sc == nil {
		s.sc = stripe.NewClient(s.cfg.Stripe.APIKey)
	}
	return s.sc
}

// billingSettingsURL is where Stripe-hosted pages send the browser back.
func (s *Service) billingSettingsURL() string {
	return s.cfg.App.PostLoginURL + "/settings/billing"
}

// CreateCheckout creates a subscription-mode Stripe Checkout session for the
// Pro plan at the org's current member count and returns its URL. The Stripe
// customer is created here on first use — trials never touch Stripe.
func (s *Service) CreateCheckout(ctx context.Context, orgID, email string) (string, error) {
	sc := s.stripeClient()
	if s.st == nil || sc == nil || s.cfg.Stripe.PriceID == "" {
		return "", ErrNotConfigured
	}
	if err := s.st.EnsureOrgBilling(ctx, orgID); err != nil {
		return "", err
	}
	b, err := s.st.GetOrgBilling(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("billing: get org billing: %w", err)
	}
	if b.StripeSubscriptionID != "" && b.StripeStatus != "canceled" {
		return "", ErrAlreadySubscribed
	}

	customerID := b.StripeCustomerID
	if customerID == "" {
		cust, err := sc.V1Customers.Create(ctx, &stripe.CustomerCreateParams{
			Email:    stripe.String(email),
			Metadata: map[string]string{"org_id": orgID},
		})
		if err != nil {
			return "", fmt.Errorf("billing: create stripe customer: %w", err)
		}
		customerID = cust.ID
		if err := s.st.SetStripeCustomer(ctx, orgID, customerID); err != nil {
			return "", err
		}
	}

	seats, err := s.seatCount(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("billing: count seats: %w", err)
	}

	// payment_method_types is intentionally NOT set: Stripe then picks the
	// eligible payment methods dynamically from dashboard settings.
	sess, err := sc.V1CheckoutSessions.Create(ctx, &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer:          stripe.String(customerID),
		ClientReferenceID: stripe.String(orgID),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Price:    stripe.String(s.cfg.Stripe.PriceID),
			Quantity: stripe.Int64(int64(seats)),
		}},
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Metadata: map[string]string{"org_id": orgID},
		},
		SuccessURL: stripe.String(s.billingSettingsURL() + "?checkout=success&session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:  stripe.String(s.billingSettingsURL() + "?checkout=cancel"),
	})
	if err != nil {
		return "", fmt.Errorf("billing: create checkout session: %w", err)
	}
	return sess.URL, nil
}

// CreatePortal creates a Stripe Billing Portal session (card updates,
// cancellation, invoices) for the org's customer and returns its URL.
func (s *Service) CreatePortal(ctx context.Context, orgID string) (string, error) {
	sc := s.stripeClient()
	if s.st == nil || sc == nil {
		return "", ErrNotConfigured
	}
	b, err := s.st.GetOrgBilling(ctx, orgID)
	if err != nil || b.StripeCustomerID == "" {
		return "", ErrNoCustomer
	}
	sess, err := sc.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(b.StripeCustomerID),
		ReturnURL: stripe.String(s.billingSettingsURL()),
	})
	if err != nil {
		return "", fmt.Errorf("billing: create portal session: %w", err)
	}
	return sess.URL, nil
}

// seatCount counts the org's active members (the Pro plan's billable
// quantity), never less than 1 — the caller themselves holds a seat.
func (s *Service) seatCount(ctx context.Context, orgID string) (int, error) {
	if s.countMembers == nil {
		return 1, nil
	}
	n, err := s.countMembers(ctx, orgID)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		n = 1
	}
	return n, nil
}

// ReconcileSeats trues the Stripe subscription quantity up (or down) to the
// org's current member count, with prorations. A no-op for orgs without a
// live subscription or when the count already matches. Called from the WorkOS
// membership webhook, invoice.upcoming, and GET /billing — recount-then-set
// keeps all three paths idempotent and order-independent.
func (s *Service) ReconcileSeats(ctx context.Context, orgID string) {
	sc := s.stripeClient()
	if s.st == nil || sc == nil {
		return
	}
	b, err := s.st.GetOrgBilling(ctx, orgID)
	if err != nil || b.StripeSubscriptionID == "" || b.StripeStatus == "canceled" {
		return
	}
	seats, err := s.seatCount(ctx, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "billing: reconcile seats: count members failed", "org_id", orgID, "error", err)
		return
	}
	if seats == b.Seats {
		return
	}
	sub, err := sc.V1Subscriptions.Retrieve(ctx, b.StripeSubscriptionID, nil)
	if err != nil || sub.Items == nil || len(sub.Items.Data) == 0 {
		slog.ErrorContext(ctx, "billing: reconcile seats: retrieve subscription failed", "org_id", orgID, "error", err)
		return
	}
	_, err = sc.V1Subscriptions.Update(ctx, b.StripeSubscriptionID, &stripe.SubscriptionUpdateParams{
		Items: []*stripe.SubscriptionUpdateItemParams{{
			ID:       stripe.String(sub.Items.Data[0].ID),
			Quantity: stripe.Int64(int64(seats)),
		}},
		ProrationBehavior: stripe.String("create_prorations"),
	})
	if err != nil {
		slog.ErrorContext(ctx, "billing: reconcile seats: update quantity failed", "org_id", orgID, "error", err)
		return
	}
	if err := s.st.SetSeats(ctx, orgID, seats); err != nil {
		slog.ErrorContext(ctx, "billing: reconcile seats: store seats failed", "org_id", orgID, "error", err)
	}
	slog.InfoContext(ctx, "billing: reconciled seats", "org_id", orgID, "seats", seats, "previous_seats", b.Seats)
}

// SubmitOSSApplication records a free open-source tier application. Fails with
// ErrAlreadySubscribed when the org is already approved or has a live
// subscription — those orgs have nothing to apply for.
func (s *Service) SubmitOSSApplication(ctx context.Context, orgID, sponsorURL, note string) (Status, error) {
	if s.st == nil {
		return Status{}, ErrNotConfigured
	}
	if err := s.st.EnsureOrgBilling(ctx, orgID); err != nil {
		return Status{}, err
	}
	b, err := s.st.GetOrgBilling(ctx, orgID)
	if err != nil {
		return Status{}, fmt.Errorf("billing: get org billing: %w", err)
	}
	if b.Plan == PlanOSSFree || (b.StripeSubscriptionID != "" && b.StripeStatus != "canceled") {
		return Status{}, ErrAlreadySubscribed
	}
	if err := s.st.SetOSSApplication(ctx, orgID, sponsorURL, note); err != nil {
		return Status{}, err
	}
	return s.StatusForOrg(ctx, orgID)
}
