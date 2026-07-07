package billing

import (
	"time"

	"xolo/backend/internal/store"
)

// Plan names as stored in org_billing.plan.
const (
	PlanTrial      = "trial"
	PlanPro        = "pro"
	PlanOSSFree    = "oss_free"
	PlanEnterprise = "enterprise"
)

// PriceCentsPerSeat is the Pro price: $9.99 per member per month.
const PriceCentsPerSeat = 999

// now is swappable in tests.
var now = time.Now

// Status is an org's derived billing state.
type Status struct {
	Plan                 string
	StripeStatus         string // raw Stripe subscription status, "" until subscribed
	TrialEndsAt          time.Time
	Seats                int  // last quantity pushed to Stripe, 0 until subscribed
	Locked               bool // true = features gated until the org subscribes
	Subscribed           bool // true = a live (non-canceled) Stripe subscription exists
	OSSApplicationStatus string
}

// derive computes the org's status from stored facts. Pure — all trial-expiry
// logic lives here so no scheduled job ever has to flip flags.
func derive(b *store.OrgBilling, at time.Time) Status {
	st := Status{
		Plan:                 b.Plan,
		StripeStatus:         b.StripeStatus,
		TrialEndsAt:          b.TrialEndsAt,
		Seats:                b.Seats,
		OSSApplicationStatus: b.OSSApplicationStatus,
	}
	st.Subscribed = b.StripeSubscriptionID != "" && b.StripeStatus != "canceled"

	trialActive := at.Before(b.TrialEndsAt)
	switch b.Plan {
	case PlanOSSFree, PlanEnterprise:
		st.Locked = false
	case PlanPro:
		// past_due stays unlocked: Stripe Smart Retries own dunning, and we
		// only lock once the subscription is actually gone.
		switch b.StripeStatus {
		case "active", "trialing", "past_due":
			st.Locked = false
		default:
			st.Locked = !trialActive
		}
	default: // trial
		st.Locked = !trialActive
	}
	return st
}
