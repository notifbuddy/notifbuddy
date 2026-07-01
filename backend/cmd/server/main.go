// Command server is the xolo backend entrypoint. It only wires dependencies and
// starts the HTTP server; all logic lives in internal/ packages.
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"

	"xolo/backend/internal/api"
	"xolo/backend/internal/auth"
	"xolo/backend/internal/config"
	"xolo/backend/internal/crypto"
	"xolo/backend/internal/httpapi"
	"xolo/backend/internal/integrations"
	"xolo/backend/internal/intent"
	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/slackapi"
	"xolo/backend/internal/store"
	syncengine "xolo/backend/internal/sync"
)

func main() {
	// Best-effort load of backend/.env so the env vars referenced by config.yaml
	// (e.g. $WORKOS_API_KEY) are present without any shell setup. Real env vars
	// already set take precedence; a missing file is not an error.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Printf("note: could not load .env (%v); relying on real environment", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("%v", err)
	}

	ctx := context.Background()

	// Persistence: connect to Postgres (if configured) and run migrations.
	// Integrations require it; if no DATABASE_URL is set we run without a store
	// and the integration endpoints report "not configured".
	var st *store.Store
	if cfg.Database.URL != "" {
		st, err = store.New(ctx, cfg.Database.URL)
		if err != nil {
			log.Fatalf("database: %v", err)
		}
		defer st.Close()
		if err := st.Migrate(ctx); err != nil {
			log.Fatalf("migrate: %v", err)
		}
		log.Printf("database connected and migrated")
	} else {
		log.Printf("note: database.url not set — integrations disabled")
	}

	// At-rest encryption for integration tokens (currently only the local
	// provider is wired; plug a KMS client into crypto.NewKMSEncryptor for prod).
	enc, err := buildEncryptor(ctx, cfg)
	if err != nil {
		log.Fatalf("encryption: %v", err)
	}

	// Auth (WorkOS): redirect handlers + the session-loading middleware.
	authSvc := auth.New(cfg)

	// Pub/sub: provider-agnostic publisher for integration events. Local dev uses
	// an in-memory bus; production selects SNS. Callers only see pubsub.Publisher.
	// When the backend is the in-memory bus we also keep the concrete *MemoryBus
	// so the sync engine can subscribe to the ingestion + processing topics.
	publisher, bus := buildPublisher(cfg)

	// Integrations (GitHub/Slack/Linear): reads the caller's org/user from the
	// session via auth.OrgUserFromRequest. Runs with a nil store when no DB is
	// configured (Enabled() == false), reporting "not configured".
	intgSvc := integrations.New(st, enc, cfg, auth.OrgUserFromRequest, publisher)

	// Sync engine: subscribes to the Linear/Slack ingestion topics and performs
	// the bidirectional sync (channel creation, comment mirroring). Only wired
	// when there is a store and an in-memory bus to subscribe on; the SNS path
	// consumes via a separate worker (not built here).
	if st != nil && bus != nil {
		classifier := intent.NewCloudflareClassifier(cfg.Cloudflare)
		engine := syncengine.New(st, slackapi.New(), intgSvc, classifier, publisher)
		bus.Subscribe(integrations.LinearWebhookTopic, engine.OnLinearEvent)
		bus.Subscribe(integrations.SlackWebhookTopic, engine.OnSlackEvent)
		log.Printf("sync engine subscribed to linear + slack ingestion topics")
	}

	// API handler (implements the ogen interface) + the generated server.
	apiHandler := httpapi.New(authSvc, intgSvc)
	srv, err := api.NewServer(apiHandler)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	// Route: browser-redirect endpoints are plain net/http handlers (302 +
	// cookies, not JSON); everything else is the spec-driven ogen server.
	// WithSession wraps everything so handlers and the integration connect
	// endpoints see the authenticated user. The integration *callbacks* rely on
	// the sealed OAuth state rather than the session, but running them under the
	// same middleware is harmless.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", authSvc.HandleLogin)
	mux.HandleFunc("GET /auth/callback", authSvc.HandleCallback)
	mux.HandleFunc("GET /auth/logout", authSvc.HandleLogout)
	mux.HandleFunc("GET /integrations/github/connect", intgSvc.HandleGitHubConnect)
	mux.HandleFunc("GET /integrations/github/callback", intgSvc.HandleGitHubCallback)
	mux.HandleFunc("POST /integrations/github/webhook", intgSvc.HandleGitHubWebhook)
	mux.HandleFunc("GET /integrations/slack/connect", intgSvc.HandleSlackConnect)
	mux.HandleFunc("GET /integrations/slack/callback", intgSvc.HandleSlackCallback)
	mux.HandleFunc("POST /integrations/slack/webhook", intgSvc.HandleSlackWebhook)
	mux.HandleFunc("GET /integrations/linear/connect", intgSvc.HandleLinearConnect)
	mux.HandleFunc("GET /integrations/linear/callback", intgSvc.HandleLinearCallback)
	mux.HandleFunc("POST /integrations/linear/webhook", intgSvc.HandleLinearWebhook)
	mux.Handle("/", srv)

	handler := httpapi.WithCORS(authSvc.WithSession(mux), cfg.CORS.AllowOrigin)

	log.Printf("listening on %s (CORS allow-origin: %s)", cfg.Server.Addr, cfg.CORS.AllowOrigin)
	if err := http.ListenAndServe(cfg.Server.Addr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// buildEncryptor constructs the at-rest Encryptor from config. The "local"
// provider uses an AES-GCM key (generating an ephemeral dev key if none is set,
// logging a warning). The "kms" provider needs a KMSClient wired in — see
// crypto.NewKMSEncryptor.
func buildEncryptor(ctx context.Context, cfg config.Config) (crypto.Encryptor, error) {
	switch cfg.Encryption.Provider {
	case "", "local":
		enc, keyB64, err := crypto.NewLocalKeyEncryptorFromBase64(cfg.Encryption.LocalKey)
		if err != nil {
			return nil, err
		}
		if cfg.Encryption.LocalKey == "" {
			log.Printf("warning: encryption.local_key not set — generated an ephemeral dev key (%d-char base64); stored tokens will not decrypt after restart", len(keyB64))
		}
		return enc, nil
	case "kms":
		// Plug your provider's KMSClient here, e.g.:
		//   client := awskms.New(...)
		//   return crypto.NewKMSEncryptor(ctx, client, cfg.Encryption.KMSKeyID)
		return nil, errWrap("encryption.provider=kms requires a KMS client to be wired into buildEncryptor")
	default:
		return nil, errWrap("unknown encryption.provider: " + cfg.Encryption.Provider)
	}
}

// buildPublisher constructs the pub/sub Publisher from config. "memory" builds
// an in-process bus and registers a logging subscriber on the webhook + sync
// topics so published events are visible in dev; it returns the concrete
// *MemoryBus too so callers (the sync engine) can subscribe. "sns" needs an AWS
// SNS client wired into pubsub.NewSNSPublisher — until then it falls back to Nop
// with a warning so the app still runs (and returns a nil bus).
func buildPublisher(cfg config.Config) (pubsub.Publisher, *pubsub.MemoryBus) {
	switch cfg.PubSub.Provider {
	case "", "memory":
		bus := pubsub.NewMemoryBus()
		logSub := func(_ context.Context, msg pubsub.Message) {
			log.Printf("pubsub(memory): %s %s", msg.Topic, string(msg.Payload))
		}
		for _, topic := range append([]string{
			integrations.GitHubWebhookTopic,
			integrations.LinearWebhookTopic,
			integrations.SlackWebhookTopic,
		}, syncengine.AllTopics...) {
			bus.Subscribe(topic, logSub)
		}
		return bus, bus
	case "sns":
		// Wire your AWS SNS client here, e.g.:
		//   client := awssns.New(...)
		//   resolver := func(string) (string, bool) { return cfg.PubSub.SNSTopicARN, cfg.PubSub.SNSTopicARN != "" }
		//   pub, err := pubsub.NewSNSPublisher(client, resolver)
		log.Printf("warning: pubsub.provider=sns but no SNS client is wired — publishing disabled (Nop). See buildPublisher.")
		return pubsub.Nop, nil
	default:
		log.Printf("warning: unknown pubsub.provider %q — publishing disabled (Nop)", cfg.PubSub.Provider)
		return pubsub.Nop, nil
	}
}

func errWrap(msg string) error { return &configError{msg} }

type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }
