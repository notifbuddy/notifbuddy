package pubsub

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// pushEnvelope is Pub/Sub's wrapped push delivery body. Data arrives base64
// in the JSON and decodes into []byte automatically.
type pushEnvelope struct {
	Message struct {
		Data        []byte            `json:"data"`
		Attributes  map[string]string `json:"attributes"`
		MessageID   string            `json:"messageId"`
		PublishTime string            `json:"publishTime"`
	} `json:"message"`
	Subscription    string `json:"subscription"` // "projects/<p>/subscriptions/<name>"
	DeliveryAttempt int    `json:"deliveryAttempt"`
}

// pushServer dispatches Pub/Sub push deliveries to registered subscriptions.
// Status codes drive Pub/Sub's ack protocol: 2xx acks; anything else nacks,
// which redelivers per the subscription's retry policy and eventually
// dead-letters to the poison topic — so an erroring handler retries exactly
// that message on exactly that subscription.
type pushServer struct {
	subs      map[string]Subscription // keyed by subscription short-name
	verify    TokenVerifier
	audience  string
	wantEmail string
}

func (s *pushServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Authenticate: Pub/Sub signs each push with an OIDC token for the
	// configured service account; nothing else may feed us events.
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	email, err := s.verify.Verify(ctx, token, s.audience)
	if err != nil {
		log.Printf("pubsub push: token rejected: %v", err)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	if email != s.wantEmail {
		log.Printf("pubsub push: token from unexpected principal %s", email)
		http.Error(w, "unexpected principal", http.StatusForbidden)
		return
	}

	var env pushEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		// Malformed body: 400 nacks it so it retries and eventually lands in
		// the dead-letter topic instead of vanishing silently.
		log.Printf("pubsub push: bad envelope: %v", err)
		http.Error(w, "bad envelope", http.StatusBadRequest)
		return
	}

	name := env.Subscription
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	sub, ok := s.subs[name]
	if !ok {
		// Unknown subscription: nack. Correct during rolling deploys — the
		// message retries until an instance that knows the subscription serves
		// it, or it dead-letters.
		log.Printf("pubsub push: no handler for subscription %q", name)
		http.Error(w, "unknown subscription", http.StatusNotFound)
		return
	}

	if err := sub.Handle(ctx, Message{
		Topic:      sub.Topic,
		Payload:    env.Message.Data,
		Attributes: env.Message.Attributes,
	}); err != nil {
		log.Printf("pubsub push: %s (attempt %d, message %s): %v", name, env.DeliveryAttempt, env.Message.MessageID, err)
		http.Error(w, "handler error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}