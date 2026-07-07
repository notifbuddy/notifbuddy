package billing

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"

	"xolo/backend/internal/store"
)

// maxStripeWebhookBody bounds a Stripe event payload read (they are small;
// 1 MiB is generous).
const maxStripeWebhookBody = 1 << 20

// HandleStripeWebhook receives a Stripe webhook delivery: verify the
// signature, store the event durably (idempotent on Stripe's event id), then
// apply it to org_billing SYNCHRONOUSLY — billing correctness must not depend
// on the pub/sub bus (a Nop in prod).
//
// Every state effect is an order-independent, idempotent upsert, so a
// redelivered or replayed event converges to the same row; the stored event is
// the audit log, not a processing gate. A failed apply returns 500 so Stripe
// redelivers and the apply runs again.
func (s *Service) HandleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if s.st == nil || s.cfg.Stripe.WebhookSecret == "" {
		http.Error(w, "billing not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxStripeWebhookBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	event, err := webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), s.cfg.Stripe.WebhookSecret)
	if err != nil {
		log.Printf("billing: stripe webhook signature verification failed: %v", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	orgID := s.resolveEventOrg(r.Context(), event)

	if _, err := s.st.InsertStripeWebhookEvent(r.Context(), store.StripeWebhookEvent{
		EventID:   event.ID,
		EventType: string(event.Type),
		OrgID:     orgID,
		Payload:   json.RawMessage(body),
	}); err != nil {
		log.Printf("billing: store stripe webhook %s: %v", event.ID, err)
		http.Error(w, "failed to store event", http.StatusInternalServerError)
		return
	}

	if orgID == "" {
		// Not ours to act on (e.g. an event for a customer we never created).
		// Stored above for the audit trail; ack so Stripe stops retrying.
		log.Printf("billing: stripe event %s (%s) resolved to no org; ignored", event.ID, event.Type)
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := s.applyStripeEvent(r.Context(), orgID, event); err != nil {
		log.Printf("billing: apply stripe event %s (%s) for %s: %v", event.ID, event.Type, orgID, err)
		http.Error(w, "failed to apply event", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// resolveEventOrg extracts the WorkOS org id from a Stripe event — from our
// metadata / client_reference_id when present, else by looking the event's
// customer up in org_billing.
func (s *Service) resolveEventOrg(ctx context.Context, event stripe.Event) string {
	var probe struct {
		Metadata          map[string]string `json:"metadata"`
		ClientReferenceID string            `json:"client_reference_id"`
		Customer          string            `json:"customer"`
	}
	if event.Data == nil || json.Unmarshal(event.Data.Raw, &probe) != nil {
		return ""
	}
	if id := probe.Metadata["org_id"]; id != "" {
		return id
	}
	if probe.ClientReferenceID != "" {
		return probe.ClientReferenceID
	}
	if probe.Customer != "" {
		if id, err := s.st.OrgIDByStripeCustomer(ctx, probe.Customer); err == nil {
			return id
		}
	}
	return ""
}

// applyStripeEvent maps one verified Stripe event onto org_billing. All
// branches are idempotent upserts keyed on org_id, so delivery order
// (checkout.session.completed vs customer.subscription.created) doesn't
// matter and replays are harmless.
func (s *Service) applyStripeEvent(ctx context.Context, orgID string, event stripe.Event) error {
	switch event.Type {
	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			return err
		}
		if sess.Customer != nil {
			if err := s.st.SetStripeCustomer(ctx, orgID, sess.Customer.ID); err != nil {
				return err
			}
		}
		if sess.Subscription == nil {
			return nil
		}
		// The subscription's real status arrives with customer.subscription.*;
		// "active" is the correct steady state for a card-paid checkout.
		return s.st.ApplySubscriptionState(ctx, orgID, store.SubscriptionState{
			SubscriptionID: sess.Subscription.ID,
			Status:         "active",
		})

	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return err
		}
		st := store.SubscriptionState{SubscriptionID: sub.ID, Status: string(sub.Status)}
		if event.Type == "customer.subscription.deleted" {
			st.Status = "canceled"
		}
		if sub.Items != nil && len(sub.Items.Data) > 0 {
			st.Seats = int(sub.Items.Data[0].Quantity)
		}
		return s.st.ApplySubscriptionState(ctx, orgID, st)

	case "invoice.paid":
		return s.st.SetStripeStatus(ctx, orgID, "active")

	case "invoice.payment_failed":
		// Grace: past_due stays unlocked while Stripe Smart Retries run. If
		// they exhaust, customer.subscription.updated/deleted moves us on.
		return s.st.SetStripeStatus(ctx, orgID, "past_due")

	case "invoice.upcoming":
		// Renewal is about to bill: true the seat quantity up so every cycle
		// charges the real member count even if no other sync path fired.
		s.ReconcileSeats(ctx, orgID)
		return nil

	default:
		// An event type we didn't subscribe to; stored, nothing to apply.
		return nil
	}
}
