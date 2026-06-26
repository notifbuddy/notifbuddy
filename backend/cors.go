package main

import "net/http"

// withCORS wraps an http.Handler with CORS headers so the browser (served by
// Vite/SvelteKit on a different origin) can call this API directly.
//
// This is *credentialed* CORS: the SPA sends the session cookie with
// `credentials: 'include'`, which requires (1) an exact allow-origin — the
// wildcard "*" is forbidden with credentials — and (2)
// Access-Control-Allow-Credentials: true. We therefore echo back exactly the
// configured origin.
func withCORS(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
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
