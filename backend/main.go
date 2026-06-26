package main

import (
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"

	"xolo/backend/internal/api"
)

func main() {
	// Best-effort load of backend/.env so the env vars referenced by
	// config.yaml (e.g. $WORKOS_API_KEY) are present without any shell setup.
	// `make dev-backend` and a bare `go run .` both pick it up. Real environment
	// variables already set take precedence — godotenv.Load does not overwrite
	// them. A missing file is not an error (prod sets real env vars).
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("note: could not load .env (%v); relying on real environment", err)
	}

	// Load YAML config and resolve its $VAR/${VAR} env references (secrets).
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("%v", err)
	}

	// Build the auth flow (redirect handlers + the session-loading middleware).
	auth := newAuthConfig(cfg)

	// Build the ogen-generated JSON server, handing it our Handler implementation.
	srv, err := api.NewServer(Handler{auth: auth})
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	// Route: the three browser-redirect auth endpoints are plain net/http
	// handlers (they 302 and set cookies, not JSON); everything else is the
	// spec-driven ogen server. withSession wraps the ogen server so /ping and
	// /me see the authenticated user; the /auth/* handlers read the cookie
	// themselves and so don't need it.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", auth.handleLogin)
	mux.HandleFunc("GET /auth/callback", auth.handleCallback)
	mux.HandleFunc("GET /auth/logout", auth.handleLogout)
	mux.Handle("/", auth.withSession(srv))

	handler := withCORS(mux, cfg.CORS.AllowOrigin)

	log.Printf("listening on %s (CORS allow-origin: %s)", cfg.Server.Addr, cfg.CORS.AllowOrigin)
	if err := http.ListenAndServe(cfg.Server.Addr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// envOr returns the value of an environment variable or a fallback. Used for
// the few bootstrap settings read before the YAML config is loaded (e.g. which
// config file to read).
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
