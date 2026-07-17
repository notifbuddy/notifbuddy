// Package config loads all application configuration from a YAML file, with
// secret fields referencing environment variables.
//
// Any string value of the form `$VAR` or `${VAR}` is expanded from the
// environment at load time; a referenced-but-unset variable is a hard error.
// Values without a `$` are used literally. This lets the YAML describe every
// field in one place while real secrets live only in the environment.
package config

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the full application configuration.
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Logging    LoggingConfig    `yaml:"logging"`
	CORS       CORSConfig       `yaml:"cors"`
	WorkOS     WorkOSConfig     `yaml:"workos"`
	App        AppConfig        `yaml:"app"`
	Database   DatabaseConfig   `yaml:"database"`
	Encryption EncryptionConfig `yaml:"encryption"`
	PubSub     PubSubConfig     `yaml:"pubsub"`
	Slack      SlackConfig      `yaml:"slack"`
	Linear     LinearConfig     `yaml:"linear"`
	Cloudflare CloudflareConfig `yaml:"cloudflare"`
	Stripe     StripeConfig     `yaml:"stripe"`
	Billing    BillingConfig    `yaml:"billing"`
}

// BillingConfig controls plan enforcement.
type BillingConfig struct {
	// Mode selects billing behavior: "live" (or empty — 21-day trials plus
	// Stripe subscriptions) or "beta" (everything free: no trial lock, no
	// checkout; status reports the "beta" plan).
	Mode string `yaml:"mode"`
}

type ServerConfig struct {
	// Addr is the listen address.
	Addr string `yaml:"addr"`
}

type LoggingConfig struct {
	// Format selects the log output: "text" (human-readable, local dev) or
	// "json" (one object per line on stdout; what Datadog/Cloud Run ingest).
	Format string `yaml:"format"`
	// Level is the minimum level emitted: "debug", "info", "warn", "error".
	Level string `yaml:"level"`
	// AxiomEnabled turns the Axiom handler on or off (default on). Axiom is
	// optional — self-hosted deployments set this false (or just leave the
	// token unset) and logs go to stdout only.
	AxiomEnabled bool `yaml:"axiom_enabled"`
	// AxiomToken, when set together with AxiomDataset (and AxiomEnabled),
	// additionally ships every record to Axiom (stdout keeps working either
	// way). SECRET — set to an env ref. Empty disables the Axiom handler.
	AxiomToken string `yaml:"axiom_token"`
	// AxiomDataset is the Axiom dataset records are ingested into.
	AxiomDataset string `yaml:"axiom_dataset"`
}

type CORSConfig struct {
	// AllowOrigin is the exact origin permitted to call the API (credentialed
	// CORS forbids "*").
	AllowOrigin string `yaml:"allow_origin"`
}

type WorkOSConfig struct {
	// ClientID is the WorkOS application client ID (client_...). Public.
	ClientID string `yaml:"client_id"`
	// APIKey is the WorkOS API key (sk_...). SECRET — set to an env ref.
	APIKey string `yaml:"api_key"`
	// CookiePassword (>=32 chars) seals the session cookie. SECRET — env ref.
	CookiePassword string `yaml:"cookie_password"`
	// RedirectURI is the OAuth callback; must match a dashboard redirect.
	RedirectURI string `yaml:"redirect_uri"`
	// LoginProvider, when set, sends users straight to one AuthKit provider.
	LoginProvider string `yaml:"login_provider"`
	// WebhookSecret verifies incoming WorkOS webhook signatures
	// (WorkOS-Signature header; organization_membership events drive seat
	// sync). SECRET — env ref. Empty disables the endpoint.
	WebhookSecret string `yaml:"webhook_secret"`
}

type AppConfig struct {
	// PostLoginURL is where the browser lands after login/logout (SPA origin).
	PostLoginURL string `yaml:"post_login_url"`
	// InsecureCookies drops the Secure flag on cookies (plain-HTTP testing).
	InsecureCookies bool `yaml:"insecure_cookies"`
}

type DatabaseConfig struct {
	// URL is the Postgres connection string. SECRET-ish — set to an env ref.
	URL string `yaml:"url"`
}

type EncryptionConfig struct {
	// Provider selects how integration tokens are encrypted at rest:
	// "local" (AES-GCM with LocalKey) or "gcpkms" (Google Cloud KMS).
	Provider string `yaml:"provider"`
	// LocalKey is a base64-encoded 32-byte key for provider=local. Empty means
	// generate an ephemeral dev key at startup (tokens won't survive a restart).
	// SECRET — set to an env ref in any shared environment.
	LocalKey string `yaml:"local_key"`
	// KMSKeyID is the crypto-key resource name for provider=gcpkms.
	KMSKeyID string `yaml:"kms_key_id"`
}

type PubSubConfig struct {
	// Provider selects the eventing backend: "postgres" (watermill over the
	// app database; local dev and bare-metal deploys) or "gcp" (Google Cloud
	// Pub/Sub push subscriptions; Cloud Run production). Empty means postgres.
	// Consumers are provider-agnostic; only wiring differs.
	Provider string         `yaml:"provider"`
	Postgres PostgresPubSub `yaml:"postgres"`
	GCP      GCPPubSub      `yaml:"gcp"`
}

type PostgresPubSub struct {
	// PollInterval is how often idle subscribers poll Postgres for new
	// messages, as a Go duration string ("200ms", "1s"). Empty defaults to 1s.
	// Lower means snappier delivery; higher means fewer queries (and on Neon,
	// polling is what keeps compute awake).
	PollInterval string `yaml:"poll_interval"`
	// LogEvents wires a dev-logger consumer group that prints every message on
	// every topic. Enable only in local dev — it doubles per-message reads.
	LogEvents bool `yaml:"log_events"`
}

type GCPPubSub struct {
	// ProjectID is the GCP project owning the Pub/Sub topics/subscriptions
	// (managed in infra/, never created by the app).
	ProjectID string `yaml:"project_id"`
	// PushAudience is the OIDC audience on push deliveries — the public URL of
	// the push endpoint (https://<backend>/internal/pubsub/push).
	PushAudience string `yaml:"push_audience"`
	// PushServiceAccount is the service account email Pub/Sub signs push
	// tokens with; deliveries from any other principal are rejected.
	PushServiceAccount string `yaml:"push_service_account"`
}

// PollIntervalDuration returns the parsed poll interval, defaulting to 1s when
// unset. validate() has already rejected unparseable or non-positive values.
func (c PostgresPubSub) PollIntervalDuration() time.Duration {
	if c.PollInterval == "" {
		return time.Second
	}
	d, _ := time.ParseDuration(c.PollInterval)
	return d
}

type SlackConfig struct {
	// ClientID / ClientSecret are the Slack app's OAuth credentials.
	// ClientSecret is SECRET — env ref.
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// Scopes is the list of bot scopes requested at OAuth.
	Scopes []string `yaml:"scopes"`
	// UserScopes are the user-token scopes requested via user_scope for a
	// user-level connection (the xoxp token under authed_user).
	UserScopes []string `yaml:"user_scopes"`
	// CallbackURL is where Slack redirects after authorize.
	CallbackURL string `yaml:"callback_url"`
	// SigningSecret verifies inbound Slack Events API requests (the
	// X-Slack-Signature v0 HMAC over the raw body). SECRET — env ref. Empty
	// disables verification (and thus the Slack → Linear sync direction).
	SigningSecret string `yaml:"signing_secret"`
}

type LinearConfig struct {
	// ClientID / ClientSecret are the Linear OAuth application credentials.
	// ClientSecret is SECRET — env ref.
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// Scopes is the list of OAuth scopes requested (e.g. read, write).
	Scopes []string `yaml:"scopes"`
	// CallbackURL is where Linear redirects after authorize.
	CallbackURL string `yaml:"callback_url"`
	// WebhookSecret verifies incoming webhook HMAC signatures
	// (Linear-Signature header). SECRET — env ref. Empty disables verification.
	WebhookSecret string `yaml:"webhook_secret"`
}

type CloudflareConfig struct {
	// AccountID is the Cloudflare account id whose Workers AI binding runs the
	// inference (the {account_id} in .../accounts/{account_id}/ai/run/...).
	AccountID string `yaml:"account_id"`
	// APIToken authorizes the Workers AI REST call (sent as a Bearer token).
	// SECRET — env ref. Empty disables NotifBuddy intent classification, which
	// then resolves every comment to "no-action".
	APIToken string `yaml:"api_token"`
	// Model is the Workers AI text-generation model id (e.g.
	// "@cf/meta/llama-3.2-1b-instruct"). Swappable without code changes.
	Model string `yaml:"model"`
}

type StripeConfig struct {
	// APIKey authorizes Stripe API calls. Use a RESTRICTED key (rk_...) with
	// write on Customers/Checkout Sessions/Subscriptions/Billing Portal only.
	// SECRET — env ref. Empty means billing checkout is not configured.
	APIKey string `yaml:"api_key"`
	// WebhookSecret verifies incoming Stripe webhook signatures
	// (Stripe-Signature header). SECRET — env ref. Empty disables the endpoint.
	WebhookSecret string `yaml:"webhook_secret"`
	// PriceID is the per-seat monthly Price (price_...) for the Pro plan,
	// created once per environment in the Stripe dashboard.
	PriceID string `yaml:"price_id"`
}

// defaultConfig returns built-in defaults applied before the YAML file is
// decoded on top. Secret/identity fields have no safe default and stay empty.
func defaultConfig() Config {
	return Config{
		Server:     ServerConfig{Addr: ":8080"},
		Logging:    LoggingConfig{Format: "text", Level: "info", AxiomEnabled: true},
		CORS:       CORSConfig{AllowOrigin: "http://localhost:5173"},
		WorkOS:     WorkOSConfig{RedirectURI: "http://localhost:8080/auth/callback"},
		App:        AppConfig{PostLoginURL: "http://localhost:5173"},
		Encryption: EncryptionConfig{Provider: "local"},
		Slack:      SlackConfig{CallbackURL: "http://localhost:8080/integrations/slack/callback"},
		Linear: LinearConfig{
			CallbackURL: "http://localhost:8080/integrations/linear/callback",
			Scopes:      []string{"read", "write"},
		},
		Cloudflare: CloudflareConfig{Model: "@cf/meta/llama-3.1-8b-instruct"},
	}
}

// Load reads the YAML config file, expands `$VAR`/`${VAR}` env references in
// every string field, and validates required values. The path comes from the
// CONFIG_FILE env var, defaulting to "config.local.yaml" (prod deploys set
// CONFIG_FILE=config.prod.yaml).
func Load() (Config, error) {
	cfg := defaultConfig()

	path := envOr("CONFIG_FILE", "config.local.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	if err := expandEnvRefs(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// envRefPattern matches a whole-value env reference: `$VAR` or `${VAR}`.
var envRefPattern = regexp.MustCompile(`^\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?$`)

// expandEnvRefs walks every string field in the config (recursively, via
// reflection) and replaces whole-value env references with the variable's
// value. A reference to an unset variable is a hard error naming the YAML path.
// Reflection keeps this correct as new string fields are added.
func expandEnvRefs(cfg *Config) error {
	return walkStrings(reflect.ValueOf(cfg).Elem(), "", func(path string, s string) (string, error) {
		return expandEnvRef(s, path)
	})
}

// walkStrings recursively visits exported string fields of a struct, calling fn
// with a dotted path (using yaml tags) and the current value, storing the
// returned value back.
func walkStrings(v reflect.Value, prefix string, fn func(path, s string) (string, error)) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fv := v.Field(i)
		name := field.Tag.Get("yaml")
		if name == "" {
			name = field.Name
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		switch fv.Kind() {
		case reflect.Struct:
			if err := walkStrings(fv, path, fn); err != nil {
				return err
			}
		case reflect.String:
			if !fv.CanSet() {
				continue
			}
			out, err := fn(path, fv.String())
			if err != nil {
				return err
			}
			fv.SetString(out)
		}
	}
	return nil
}

// expandEnvRef resolves a single value. If it is an env reference ($VAR/${VAR}),
// it returns the variable's value, or empty if that variable is unset. Otherwise
// it returns the value unchanged.
//
// Unset references expand to "" rather than erroring: optional integration
// fields (Slack/Linear/DB) are routinely left unconfigured, and those flows
// report "not configured" at use. The genuinely-required fields are enforced
// afterwards by validate(), which produces a precise message.
func expandEnvRef(value, fieldName string) (string, error) {
	m := envRefPattern.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return value, nil
	}
	return os.Getenv(m[1]), nil
}

// validate checks required fields after expansion. Integration fields are
// optional — those flows simply report "not configured" when blank — so only
// the WorkOS essentials are required here.
func (c *Config) validate() error {
	if c.WorkOS.ClientID == "" {
		return fmt.Errorf("workos.client_id is required")
	}
	if c.WorkOS.APIKey == "" {
		return fmt.Errorf("workos.api_key is required (e.g. set it to $WORKOS_API_KEY)")
	}
	if c.WorkOS.CookiePassword == "" {
		return fmt.Errorf("workos.cookie_password is required (e.g. set it to $WORKOS_COOKIE_PASSWORD)")
	}
	if len(c.WorkOS.CookiePassword) < 32 {
		return fmt.Errorf("workos.cookie_password must be at least 32 characters (got %d)", len(c.WorkOS.CookiePassword))
	}
	// When an integration's OAuth app is configured (its client secret is set),
	// its inbound webhook secret is mandatory. The webhook handlers fail closed
	// without it (refusing every event); catch the misconfiguration at boot
	// rather than silently dropping — or, before this was fail-closed, silently
	// accepting forged — provider webhooks.
	if c.Slack.ClientSecret != "" && c.Slack.SigningSecret == "" {
		return fmt.Errorf("slack.signing_secret is required when Slack is configured (e.g. set it to $SLACK_SIGNING_SECRET)")
	}
	if c.Linear.ClientSecret != "" && c.Linear.WebhookSecret == "" {
		return fmt.Errorf("linear.webhook_secret is required when Linear is configured (e.g. set it to $LINEAR_WEBHOOK_SECRET)")
	}
	switch c.Logging.Format {
	case "", "text", "json":
	default:
		return fmt.Errorf("unknown logging.format %q (want text or json)", c.Logging.Format)
	}
	switch c.Logging.Level {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unknown logging.level %q (want debug, info, warn, or error)", c.Logging.Level)
	}
	if (c.Logging.AxiomToken == "") != (c.Logging.AxiomDataset == "") {
		return fmt.Errorf("logging.axiom_token and logging.axiom_dataset must be set together")
	}
	switch c.PubSub.Provider {
	case "", "postgres":
		if pi := c.PubSub.Postgres.PollInterval; pi != "" {
			d, err := time.ParseDuration(pi)
			if err != nil {
				return fmt.Errorf("pubsub.postgres.poll_interval: %w", err)
			}
			if d <= 0 {
				return fmt.Errorf("pubsub.postgres.poll_interval must be positive (got %s)", d)
			}
		}
	case "gcp":
		g := c.PubSub.GCP
		if g.ProjectID == "" || g.PushAudience == "" || g.PushServiceAccount == "" {
			return fmt.Errorf("pubsub.provider=gcp requires pubsub.gcp.project_id, push_audience, and push_service_account")
		}
	default:
		return fmt.Errorf("unknown pubsub.provider %q (want postgres or gcp)", c.PubSub.Provider)
	}
	return nil
}

// envOr returns the value of an environment variable or a fallback. Used for
// the bootstrap CONFIG_FILE lookup before the YAML is loaded.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
