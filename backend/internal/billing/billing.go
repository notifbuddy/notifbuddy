// Package billing owns plans, the 21-day card-less trial, and the Stripe
// subscription lifecycle. WorkOS owns orgs and memberships, so billing state
// keys on the WorkOS org id; Postgres stores facts (plan, raw Stripe status,
// trial deadline) and lock state is derived at read time — there is no cron.
//
// Stripe objects exist only once an org checks out: the trial runs entirely in
// our database. Paid state then mirrors Stripe via webhooks.
package billing

import (
	"context"
	"fmt"

	"github.com/stripe/stripe-go/v86"

	"xolo/backend/internal/config"
	"xolo/backend/internal/store"
)

// MemberCounter reports how many active members an org has. It lets this
// package count seats without importing auth (mirrors integrations'
// SessionResolver adapter); the wiring passes an adapter over
// auth.Service.ListOrganizationMembers in.
type MemberCounter func(ctx context.Context, orgID string) (int, error)

// Service orchestrates billing. A nil store means billing is not configured
// (no database); callers get ErrNotConfigured rather than panics.
type Service struct {
	st           *store.Store
	cfg          config.Config
	countMembers MemberCounter
	sc           *stripe.Client // lazily built from cfg.Stripe.APIKey
}

// ErrNotConfigured reports that billing has no database or Stripe config.
var ErrNotConfigured = fmt.Errorf("billing: not configured")

// New builds the billing service. st may be nil when the app runs without a
// database.
func New(st *store.Store, cfg config.Config, countMembers MemberCounter) *Service {
	return &Service{st: st, cfg: cfg, countMembers: countMembers}
}

// EnsureOrg lazily creates the org's billing row with a fresh 21-day trial.
// Orgs are created inside WorkOS (no creation hook reaches us), so the first
// authenticated touch starts the clock.
func (s *Service) EnsureOrg(ctx context.Context, orgID string) error {
	if s.st == nil {
		return ErrNotConfigured
	}
	return s.st.EnsureOrgBilling(ctx, orgID)
}

// StatusForOrg returns the org's billing status, creating the row (and
// starting the trial) on first touch.
func (s *Service) StatusForOrg(ctx context.Context, orgID string) (Status, error) {
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
	return derive(b, now()), nil
}
