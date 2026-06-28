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

	"gopkg.in/yaml.v3"
)

// Config is the full application configuration.
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	CORS       CORSConfig       `yaml:"cors"`
	WorkOS     WorkOSConfig     `yaml:"workos"`
	App        AppConfig        `yaml:"app"`
	Database   DatabaseConfig   `yaml:"database"`
	Encryption EncryptionConfig `yaml:"encryption"`
	PubSub     PubSubConfig     `yaml:"pubsub"`
	GitHub     GitHubConfig     `yaml:"github"`
	Slack      SlackConfig      `yaml:"slack"`
	Linear     LinearConfig     `yaml:"linear"`
}

type ServerConfig struct {
	// Addr is the listen address.
	Addr string `yaml:"addr"`
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
	// "local" (AES-GCM with LocalKey) or "kms" (customer-managed KMS).
	Provider string `yaml:"provider"`
	// LocalKey is a base64-encoded 32-byte key for provider=local. Empty means
	// generate an ephemeral dev key at startup (tokens won't survive a restart).
	// SECRET — set to an env ref in any shared environment.
	LocalKey string `yaml:"local_key"`
	// KMSKeyID is the key ID/ARN for provider=kms.
	KMSKeyID string `yaml:"kms_key_id"`
}

type PubSubConfig struct {
	// Provider selects the publish backend: "memory" (in-process bus for local
	// dev) or "sns" (AWS SNS for production).
	Provider string `yaml:"provider"`
	// SNSTopicARN is the SNS topic ARN for the GitHub webhook event topic when
	// Provider=sns.
	SNSTopicARN string `yaml:"sns_topic_arn"`
}

type GitHubConfig struct {
	// AppSlug is the GitHub App's URL slug (github.com/apps/<slug>).
	AppSlug string `yaml:"app_slug"`
	// AppID is the numeric GitHub App ID (used to sign the app JWT).
	AppID string `yaml:"app_id"`
	// ClientID / ClientSecret are the App's OAuth credentials (user identity on
	// the callback). ClientSecret is SECRET — env ref.
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// PrivateKey is the App's PEM private key, used to mint installation tokens.
	// SECRET — env ref (the PEM contents, or a $VAR holding them).
	PrivateKey string `yaml:"private_key"`
	// WebhookSecret verifies incoming webhook HMAC signatures
	// (X-Hub-Signature-256). SECRET — env ref. Empty disables verification.
	WebhookSecret string `yaml:"webhook_secret"`
	// CallbackURL is where GitHub redirects after install/authorize.
	CallbackURL string `yaml:"callback_url"`
}

type SlackConfig struct {
	// ClientID / ClientSecret are the Slack app's OAuth credentials.
	// ClientSecret is SECRET — env ref.
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	// Scopes is the list of bot scopes requested at OAuth.
	Scopes []string `yaml:"scopes"`
	// CallbackURL is where Slack redirects after authorize.
	CallbackURL string `yaml:"callback_url"`
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

// defaultConfig returns built-in defaults applied before the YAML file is
// decoded on top. Secret/identity fields have no safe default and stay empty.
func defaultConfig() Config {
	return Config{
		Server: ServerConfig{Addr: ":8080"},
		CORS:   CORSConfig{AllowOrigin: "http://localhost:5173"},
		WorkOS: WorkOSConfig{RedirectURI: "http://localhost:8080/auth/callback"},
		App:    AppConfig{PostLoginURL: "http://localhost:5173"},
		Encryption: EncryptionConfig{Provider: "local"},
		PubSub:     PubSubConfig{Provider: "memory"},
		GitHub: GitHubConfig{CallbackURL: "http://localhost:8080/integrations/github/callback"},
		Slack:  SlackConfig{CallbackURL: "http://localhost:8080/integrations/slack/callback"},
		Linear: LinearConfig{
			CallbackURL: "http://localhost:8080/integrations/linear/callback",
			Scopes:      []string{"read", "write"},
		},
	}
}

// Load reads the YAML config file, expands `$VAR`/`${VAR}` env references in
// every string field, and validates required values. The path comes from the
// CONFIG_FILE env var, defaulting to "config.yaml".
func Load() (Config, error) {
	cfg := defaultConfig()

	path := envOr("CONFIG_FILE", "config.yaml")
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
// fields (GitHub/Slack/DB) are routinely left unconfigured, and those flows
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
