// Package httpapi implements the ogen-generated api.Handler interface for the
// waitlist service. Everything else (routing, decoding, encoding, validation)
// is generated from the spec.
package httpapi

import (
	"context"
	"log"
	"net/http"
	"net/mail"
	"strings"

	"xolo/waitlist-backend/internal/api"
	"xolo/waitlist-backend/internal/store"
)

// Handler implements the ogen-generated api.Handler interface.
type Handler struct {
	store *store.Store
}

// New builds the API handler with its store dependency.
func New(st *store.Store) *Handler {
	return &Handler{store: st}
}

// JoinWaitlist implements the `joinWaitlist` operation: POST /waitlist.
// Public by design — the whole service is the landing page's waitlist form.
func (h Handler) JoinWaitlist(ctx context.Context, req *api.WaitlistRequest) (api.JoinWaitlistRes, error) {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if _, err := mail.ParseAddress(email); err != nil || strings.ContainsAny(email, " <>") {
		return &api.Error{Message: "enter a valid email address"}, nil
	}
	if err := h.store.AddToWaitlist(ctx, email); err != nil {
		log.Printf("httpapi: waitlist signup for %s: %v", email, err)
		return &api.Error{Message: "the waitlist is temporarily unavailable — try again shortly"}, nil
	}
	return &api.WaitlistResponse{Message: "you're on the list"}, nil
}

// WithCORS allows the landing page's origin to call this API. Same middleware
// as the main backend, minus credentials — the waitlist is cookieless.
func WithCORS(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Vary", "Origin")

		// Short-circuit CORS preflight requests.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
