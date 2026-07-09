// Command server is the waitlist service entrypoint. It only wires
// dependencies and starts the HTTP server; all logic lives in internal/
// packages. Deliberately separate from the main backend so the landing page's
// waitlist can go live (against its own Neon database) before the product
// backend is deployed.
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"

	"xolo/waitlist-backend/internal/api"
	"xolo/waitlist-backend/internal/config"
	"xolo/waitlist-backend/internal/httpapi"
	"xolo/waitlist-backend/internal/store"
)

func main() {
	// Best-effort load of waitlist-backend/.env so the env vars referenced by
	// the config (e.g. $WAITLIST_DATABASE_URL) are present without any shell
	// setup. Real env vars already set take precedence.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("note: could not load .env (%v); relying on real environment", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("%v", err)
	}

	ctx := context.Background()

	st, err := store.New(ctx, cfg.Database.URL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Printf("database connected and migrated")

	srv, err := api.NewServer(httpapi.New(st))
	if err != nil {
		log.Fatalf("api server: %v", err)
	}

	handler := httpapi.WithCORS(srv, cfg.CORS.AllowOrigin)
	log.Printf("waitlist service listening on %s (CORS allow-origin: %s)", cfg.Server.Addr, cfg.CORS.AllowOrigin)
	if err := http.ListenAndServe(cfg.Server.Addr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}
