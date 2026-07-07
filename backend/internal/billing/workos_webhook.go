package billing

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	workos "github.com/workos/workos-go/v9"

	"xolo/backend/internal/store"
)

// maxWorkOSWebhookBody bounds a WorkOS event payload read.
const maxWorkOSWebhookBody = 1 << 20

// HandleWorkOSWebhook receives WorkOS webhook deliveries. We subscribe only to
// organization_membership.created/updated/deleted — membership changes are the
// seat-count signal Stripe can't see, so each one trues the org's subscription
// quantity up. Deliveries are stored idempotently on the WorkOS event id;
// reconciliation is recount-then-set, so replays and out-of-order deliveries
// converge.
func (s *Service) HandleWorkOSWebhook(w http.ResponseWriter, r *http.Request) {
	secret := s.cfg.WorkOS.WebhookSecret
	if s.st == nil || secret == "" {
		http.Error(w, "workos webhooks not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWorkOSWebhookBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	event, err := workos.NewWebhookVerifier(secret).ConstructEvent(r.Header.Get("WorkOS-Signature"), string(body))
	if err != nil {
		log.Printf("billing: workos webhook signature verification failed: %v", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	orgID := workosEventOrg(event.Data)

	inserted, err := s.st.InsertWorkOSWebhookEvent(r.Context(), store.WorkOSWebhookEvent{
		EventID:   event.ID,
		EventType: event.Event,
		OrgID:     orgID,
		Payload:   json.RawMessage(body),
	})
	if err != nil {
		log.Printf("billing: store workos webhook %s: %v", event.ID, err)
		http.Error(w, "failed to store event", http.StatusInternalServerError)
		return
	}
	if !inserted {
		// Redelivery of an already-stored event: reconciliation is idempotent
		// anyway, so just ack.
		w.WriteHeader(http.StatusOK)
		return
	}

	switch event.Event {
	case "organization_membership.created",
		"organization_membership.updated",
		"organization_membership.deleted":
		if orgID != "" {
			s.ReconcileSeats(r.Context(), orgID)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// workosEventOrg pulls the organization id out of an event payload.
func workosEventOrg(data map[string]any) string {
	if id, ok := data["organization_id"].(string); ok {
		return id
	}
	return ""
}
