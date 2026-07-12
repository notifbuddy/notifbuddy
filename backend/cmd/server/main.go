// Command server is the xolo backend entrypoint. It only wires dependencies and
// starts the HTTP server; all logic lives in internal/ packages.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"xolo/backend/internal/api"
	"xolo/backend/internal/auth"
	"xolo/backend/internal/billing"
	"xolo/backend/internal/config"
	"xolo/backend/internal/crypto"
	"xolo/backend/internal/httpapi"
	"xolo/backend/internal/integrations"
	"xolo/backend/internal/intent"
	"xolo/backend/internal/logging"
	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/slackapi"
	"xolo/backend/internal/store"
	syncengine "xolo/backend/internal/sync"
)

func main() {
	// Best-effort load of backend/.env so the env vars referenced by the config
	// (e.g. $WORKOS_API_KEY) are present without any shell setup. Real env vars
	// already set take precedence; a missing file is not an error.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("could not load .env; relying on real environment", "error", err)
	}

	cfg, err := config.Load()
	if err != nil {
		fatal("config", err)
	}

	// Structured logging (log/slog): JSON in prod (Datadog-parseable), text in
	// dev. Installed as the slog default; the stdlib log package routes through
	// it too, so third-party log output also comes out structured.
	logging.Setup(cfg.Logging.Format, cfg.Logging.Level)

	// Root context: canceled on SIGINT/SIGTERM, which drives the graceful
	// shutdown of both the HTTP server and the pub/sub router.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Persistence: connect to Postgres (if configured) and run migrations.
	// Integrations require it; if no DATABASE_URL is set we run without a store
	// and the integration endpoints report "not configured".
	var st *store.Store
	if cfg.Database.URL != "" {
		st, err = store.New(ctx, cfg.Database.URL)
		if err != nil {
			fatal("database connect", err)
		}
		defer st.Close()
		if err := st.Migrate(ctx); err != nil {
			fatal("database migrate", err)
		}
		slog.Info("database connected and migrated")
	} else {
		slog.Warn("database.url not set — integrations disabled")
	}

	// At-rest encryption for integration tokens: local AES-GCM for dev,
	// Google Cloud KMS in prod (encryption.provider in config).
	enc, err := buildEncryptor(ctx, cfg)
	if err != nil {
		fatal("encryption", err)
	}

	// Auth (WorkOS): redirect handlers + the session-loading middleware.
	authSvc := auth.New(cfg)

	// Pub/sub: the provider-agnostic eventing bus (postgres/watermill or GCP
	// Pub/Sub push, per pubsub.provider). Publishers and consumers see only
	// the pubsub abstractions. Without a database we run degraded with
	// pubsub.Nop and no consumers (webhook endpoints already report "not
	// configured" in that mode).
	var publisher pubsub.Publisher = pubsub.Nop
	var bus pubsub.Bus
	if st != nil {
		bus, err = pubsub.NewBus(ctx, cfg.PubSub, st.Pool())
		if err != nil {
			fatal("pubsub", err)
		}
		publisher = bus
	}

	// Integrations (Slack/Linear): reads the caller's org/user from the
	// session via auth.OrgUserFromRequest. Runs with a nil store when no DB is
	// configured (Enabled() == false), reporting "not configured".
	intgSvc := integrations.New(st, enc, cfg, auth.OrgUserFromRequest, publisher)

	// Billing (Stripe): 21-day trials + per-seat Pro subscriptions. Seat counts
	// come from WorkOS org memberships via the auth service. Runs with a nil
	// store when no DB is configured, reporting "not configured".
	billingSvc := billing.New(st, cfg, func(ctx context.Context, orgID string) (int, error) {
		members, err := authSvc.ListOrganizationMembers(ctx, orgID, 200)
		if err != nil {
			return 0, err
		}
		return len(members), nil
	})

	// Consumers: the writer (persists raw webhook deliveries, then fires the
	// processed topic) and the sync engine (bidirectional Slack<->Linear sync).
	// Each Subscription is an independent consumer — every one receives every
	// message of its topic, with its own retry state, on both providers.
	if bus != nil {
		classifier := intent.NewCloudflareClassifier(cfg.Cloudflare)
		// Billing enforcement: locked orgs (expired trial, no subscription)
		// have their inbound events dropped instead of synced.
		orgLocked := func(ctx context.Context, orgID string) bool {
			status, err := billingSvc.StatusForOrg(ctx, orgID)
			if err != nil {
				return false // never let a billing hiccup break the product path
			}
			return status.Locked
		}
		engine := syncengine.New(st, slackapi.New(), intgSvc, classifier, publisher, orgLocked)

		// The topology (topics + subscriptions with their topics/groups) lives
		// in internal/pubsub/manifest.yaml, shared with infra; this map
		// only binds a handler to each subscription name. BindSubscriptions
		// fails on any mismatch in either direction.
		subs, err := pubsub.BindSubscriptions(map[string]pubsub.Handler{
			"writer-linear": intgSvc.WriteLinearWebhook,
			"writer-slack":  intgSvc.WriteSlackWebhook,
			"sync-linear":   engine.OnLinearEvent,
			"sync-slack":    engine.OnSlackEvent,
		})
		if err != nil {
			fatal("pubsub subscriptions", err)
		}
		if err := bus.Start(ctx, subs); err != nil {
			fatal("pubsub start", err)
		}
		slog.Info("pubsub consumers running", "provider", cfg.PubSub.Provider)
	}

	// API handler (implements the ogen interface) + the generated server.
	apiHandler := httpapi.New(authSvc, intgSvc, billingSvc, st)
	srv, err := api.NewServer(apiHandler)
	if err != nil {
		fatal("create api server", err)
	}

	// Route: browser-redirect endpoints are plain net/http handlers (302 +
	// cookies, not JSON); everything else is the spec-driven ogen server.
	// WithSession wraps everything so handlers and the integration connect
	// endpoints see the authenticated user. The integration *callbacks* rely on
	// the sealed OAuth state rather than the session, but running them under the
	// same middleware is harmless.
	// gateBilling wraps a browser-redirect handler so locked orgs (expired
	// trial, no subscription) get a 402 instead of connecting integrations.
	gateBilling := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if orgID, _ := auth.OrgUserFromRequest(r); orgID != "" {
				if status, err := billingSvc.StatusForOrg(r.Context(), orgID); err == nil && status.Locked {
					http.Error(w, "trial expired — subscribe to keep using NotifBuddy", http.StatusPaymentRequired)
					return
				}
			}
			next(w, r)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", authSvc.HandleLogin)
	mux.HandleFunc("GET /auth/callback", authSvc.HandleCallback)
	mux.HandleFunc("GET /auth/logout", authSvc.HandleLogout)
	mux.HandleFunc("GET /integrations/slack/connect", gateBilling(intgSvc.HandleSlackConnect))
	mux.HandleFunc("GET /integrations/slack/callback", intgSvc.HandleSlackCallback)
	mux.HandleFunc("POST /integrations/slack/webhook", intgSvc.HandleSlackWebhook)
	mux.HandleFunc("GET /integrations/linear/connect", gateBilling(intgSvc.HandleLinearConnect))
	mux.HandleFunc("GET /integrations/linear/callback", intgSvc.HandleLinearCallback)
	mux.HandleFunc("POST /integrations/linear/webhook", intgSvc.HandleLinearWebhook)
	mux.HandleFunc("POST /billing/stripe/webhook", billingSvc.HandleStripeWebhook)
	mux.HandleFunc("POST /auth/workos/webhook", billingSvc.HandleWorkOSWebhook)
	// Push-based pub/sub providers (gcp) deliver messages here; the handler
	// does its own OIDC auth, like the provider webhooks above do signatures.
	if bus != nil {
		if push := bus.PushHandler(); push != nil {
			mux.Handle("POST "+pubsub.PushPath, push)
		}
	}
	mux.Handle("/", srv)

	handler := httpapi.WithRequestLog(httpapi.WithCORS(authSvc.WithSession(mux), cfg.CORS.AllowOrigin))

	// Consumers are already live (bus.Start above), so serve HTTP. On
	// SIGINT/SIGTERM: stop accepting HTTP (no new publishes, and on gcp no
	// new push deliveries), then close the bus (drains consumers, flushes
	// publishers), then the deferred st.Close() releases the pool.
	httpSrv := &http.Server{Addr: cfg.Server.Addr, Handler: handler}
	go func() {
		slog.Info("listening", "addr", cfg.Server.Addr, "cors_allow_origin", cfg.CORS.AllowOrigin)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("http server", err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown", "error", err)
	}
	if bus != nil {
		if err := bus.Close(); err != nil {
			slog.Error("pubsub close", "error", err)
		}
	}
}

// fatal is the slog replacement for log.Fatalf: log at error level, then exit.
func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

// buildEncryptor constructs the at-rest Encryptor from config. The "local"
// provider uses an AES-GCM key (generating an ephemeral dev key if none is set,
// logging a warning). The "gcpkms" provider encrypts against a Google Cloud KMS
// key (created by infra) via Application Default Credentials.
func buildEncryptor(ctx context.Context, cfg config.Config) (crypto.Encryptor, error) {
	switch cfg.Encryption.Provider {
	case "", "local":
		enc, keyB64, err := crypto.NewLocalKeyEncryptorFromBase64(cfg.Encryption.LocalKey)
		if err != nil {
			return nil, err
		}
		if cfg.Encryption.LocalKey == "" {
			slog.Warn("encryption.local_key not set — generated an ephemeral dev key; stored tokens will not decrypt after restart", "key_base64_len", len(keyB64))
		}
		return enc, nil
	case "gcpkms":
		// The client lives for the process — no explicit Close. KMSKeyID is
		// the crypto key's full resource name; Decrypt resolves the version
		// from each ciphertext, so rotation needs no app changes.
		client, err := crypto.NewGCPKMSClient(ctx)
		if err != nil {
			return nil, err
		}
		return crypto.NewKMSEncryptor(ctx, client, cfg.Encryption.KMSKeyID)
	default:
		return nil, errWrap("unknown encryption.provider: " + cfg.Encryption.Provider)
	}
}

func errWrap(msg string) error { return &configError{msg} }

type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }
