// Package respond holds the tiny HTTP helpers the provider fakes share.
package respond

import (
	"encoding/json"
	"log"
	"net/http"
)

// JSON writes body as a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// NotImplemented is a placeholder handler for a provider that isn't faked yet.
// It answers 501 loudly (and logs) so the first call a flow makes is impossible
// to miss.
func NotImplemented(provider string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("fakeapis: %s not implemented: %s %s", provider, r.Method, r.URL.Path)
		JSON(w, http.StatusNotImplemented, map[string]string{"error": "fakeapis: " + provider + " fake not implemented"})
	})
}
