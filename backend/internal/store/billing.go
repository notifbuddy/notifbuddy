package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// OrgBilling is an org's billing row. Plan and StripeStatus are stored facts;
// whether the org is locked is derived at read time by the billing package.
type OrgBilling struct {
	OrgID                string
	Plan                 string // trial | pro | oss_free | enterprise
	StripeCustomerID     string
	StripeSubscriptionID string
	StripeStatus         string // raw Stripe subscription status, "" until subscribed
	Seats                int    // last quantity pushed to Stripe, 0 until subscribed
	TrialEndsAt          time.Time
	SponsorURL           string
	SponsorNote          string
	OSSApplicationStatus string // "" | pending | approved | rejected
}

// EnsureOrgBilling lazily creates an org's billing row with a fresh 21-day
// trial. Orgs are created inside WorkOS (no creation hook reaches us), so the
// trial clock starts on the org's first authenticated touch. A no-op when the
// row already exists.
func (s *Store) EnsureOrgBilling(ctx context.Context, orgID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO org_billing (org_id, trial_ends_at)
		VALUES ($1, now() + interval '21 days')
		ON CONFLICT (org_id) DO NOTHING
	`, orgID)
	if err != nil {
		return fmt.Errorf("store: ensure org billing: %w", err)
	}
	return nil
}

// GetOrgBilling returns an org's billing row, or ErrNotFound.
func (s *Store) GetOrgBilling(ctx context.Context, orgID string) (*OrgBilling, error) {
	var b OrgBilling
	err := s.pool.QueryRow(ctx, `
		SELECT org_id, plan, coalesce(stripe_customer_id,''),
		       coalesce(stripe_subscription_id,''), coalesce(stripe_status,''),
		       coalesce(seats,0), trial_ends_at, coalesce(sponsor_url,''),
		       coalesce(sponsor_note,''), coalesce(oss_application_status,'')
		FROM org_billing
		WHERE org_id = $1
	`, orgID).Scan(&b.OrgID, &b.Plan, &b.StripeCustomerID, &b.StripeSubscriptionID,
		&b.StripeStatus, &b.Seats, &b.TrialEndsAt, &b.SponsorURL, &b.SponsorNote,
		&b.OSSApplicationStatus)
	if err != nil {
		return nil, ErrNotFound
	}
	return &b, nil
}

// SetStripeCustomer records the org's Stripe customer id (created at first
// checkout). Idempotent: re-setting the same id is a no-op.
func (s *Store) SetStripeCustomer(ctx context.Context, orgID, customerID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE org_billing
		SET stripe_customer_id = $2, updated_at = now()
		WHERE org_id = $1
	`, orgID, customerID)
	if err != nil {
		return fmt.Errorf("store: set stripe customer: %w", err)
	}
	return nil
}

// SubscriptionState is the subset of Stripe subscription facts we mirror.
type SubscriptionState struct {
	SubscriptionID string
	Status         string // raw Stripe status; "canceled" clears the plan back to trial
	Seats          int    // 0 = leave the stored seat count unchanged
}

// ApplySubscriptionState upserts the org's mirrored Stripe subscription facts.
// Writes are order-independent: whichever of checkout.session.completed /
// customer.subscription.* arrives first produces the same end state.
func (s *Store) ApplySubscriptionState(ctx context.Context, orgID string, st SubscriptionState) error {
	// A canceled subscription drops the org back to the trial plan (usually
	// already expired, so the org locks); anything else marks it pro.
	plan := "pro"
	subID := st.SubscriptionID
	if st.Status == "canceled" {
		plan = "trial"
		subID = ""
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE org_billing
		SET stripe_subscription_id = NULLIF($2,''),
		    stripe_status          = $3,
		    seats                  = CASE WHEN $4 > 0 THEN $4 ELSE seats END,
		    plan                   = CASE WHEN plan IN ('oss_free','enterprise') THEN plan ELSE $5 END,
		    updated_at             = now()
		WHERE org_id = $1
	`, orgID, subID, st.Status, st.Seats, plan)
	if err != nil {
		return fmt.Errorf("store: apply subscription state: %w", err)
	}
	return nil
}

// SetStripeStatus updates only the mirrored Stripe status (invoice.paid /
// invoice.payment_failed), and only while a subscription is attached — a
// stray invoice event for a long-gone subscription must not resurrect state.
func (s *Store) SetStripeStatus(ctx context.Context, orgID, status string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE org_billing
		SET stripe_status = $2, updated_at = now()
		WHERE org_id = $1 AND stripe_subscription_id IS NOT NULL
	`, orgID, status)
	if err != nil {
		return fmt.Errorf("store: set stripe status: %w", err)
	}
	return nil
}

// SetSeats records the seat quantity last pushed to Stripe.
func (s *Store) SetSeats(ctx context.Context, orgID string, seats int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE org_billing SET seats = $2, updated_at = now() WHERE org_id = $1
	`, orgID, seats)
	if err != nil {
		return fmt.Errorf("store: set seats: %w", err)
	}
	return nil
}

// SetOSSApplication records a pending open-source free-tier application.
// Approval is manual (documented SQL flips plan to oss_free).
func (s *Store) SetOSSApplication(ctx context.Context, orgID, sponsorURL, note string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE org_billing
		SET sponsor_url = $2, sponsor_note = NULLIF($3,''),
		    oss_application_status = 'pending', oss_applied_at = now(),
		    updated_at = now()
		WHERE org_id = $1
	`, orgID, sponsorURL, note)
	if err != nil {
		return fmt.Errorf("store: set oss application: %w", err)
	}
	return nil
}

// OrgIDByStripeCustomer resolves the org that owns a Stripe customer id, or
// "" + ErrNotFound. Fallback for webhook events that lack our org metadata.
func (s *Store) OrgIDByStripeCustomer(ctx context.Context, customerID string) (string, error) {
	var orgID string
	err := s.pool.QueryRow(ctx, `
		SELECT org_id FROM org_billing WHERE stripe_customer_id = $1
	`, customerID).Scan(&orgID)
	if err != nil {
		return "", ErrNotFound
	}
	return orgID, nil
}

// StripeWebhookEvent is one stored Stripe webhook delivery.
type StripeWebhookEvent struct {
	ID         int64
	EventID    string
	EventType  string
	OrgID      string
	Payload    json.RawMessage
	ReceivedAt string
}

// InsertStripeWebhookEvent stores a received Stripe event. On a duplicate
// event id (Stripe retry) it does nothing and reports inserted=false, so the
// caller can treat retries idempotently.
func (s *Store) InsertStripeWebhookEvent(ctx context.Context, e StripeWebhookEvent) (inserted bool, err error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO stripe_webhook_events (event_id, event_type, org_id, payload)
		VALUES ($1, $2, NULLIF($3,''), $4)
		ON CONFLICT (event_id) DO NOTHING
	`, e.EventID, e.EventType, e.OrgID, []byte(e.Payload))
	if err != nil {
		return false, fmt.Errorf("store: insert stripe webhook event: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// WorkOSWebhookEvent is one stored WorkOS webhook delivery.
type WorkOSWebhookEvent struct {
	ID         int64
	EventID    string
	EventType  string
	OrgID      string
	Payload    json.RawMessage
	ReceivedAt string
}

// InsertWorkOSWebhookEvent stores a received WorkOS event. On a duplicate
// event id (WorkOS retry) it does nothing and reports inserted=false, so the
// caller can treat retries idempotently.
func (s *Store) InsertWorkOSWebhookEvent(ctx context.Context, e WorkOSWebhookEvent) (inserted bool, err error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO workos_webhook_events (event_id, event_type, org_id, payload)
		VALUES ($1, $2, NULLIF($3,''), $4)
		ON CONFLICT (event_id) DO NOTHING
	`, e.EventID, e.EventType, e.OrgID, []byte(e.Payload))
	if err != nil {
		return false, fmt.Errorf("store: insert workos webhook event: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
