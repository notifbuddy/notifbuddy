package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration, loaded from a YAML file
// (config.yaml by default).
//
// Secrets are kept out of the committed file by referencing environment
// variables: any string value of the form `$VAR` or `${VAR}` is expanded from
// the environment at load time (see expandEnvRefs). A referenced variable that
// is unset is a hard error. Values without a `$` are used literally.
//
// This lets the YAML describe every field in one place while real secrets
// (API key, cookie password) live only in the environment.
type Config struct {
	Server ServerConfig `yaml:"server"`
	CORS   CORSConfig   `yaml:"cors"`
	WorkOS WorkOSConfig `yaml:"workos"`
	App    AppConfig    `yaml:"app"`
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
	// ClientID is the WorkOS application client ID (client_...). Public
	// identifier — fine to commit, but may also be an env ref.
	ClientID string `yaml:"client_id"`
	// APIKey is the WorkOS API key (sk_...). SECRET — set this to an env ref,
	// e.g. `$WORKOS_API_KEY`.
	APIKey string `yaml:"api_key"`
	// CookiePassword (>=32 chars) seals the session cookie. SECRET — set this to
	// an env ref, e.g. `$WORKOS_COOKIE_PASSWORD`.
	CookiePassword string `yaml:"cookie_password"`
	// RedirectURI is the OAuth callback; must match a dashboard redirect.
	RedirectURI string `yaml:"redirect_uri"`
	// LoginProvider, when set, sends users straight to one AuthKit provider
	// (e.g. "GitHubOAuth") instead of the hosted selector.
	LoginProvider string `yaml:"login_provider"`
}

type AppConfig struct {
	// PostLoginURL is where the browser lands after login/logout (the SPA origin).
	PostLoginURL string `yaml:"post_login_url"`
	// InsecureCookies drops the Secure flag on the session cookie (only for
	// plain-HTTP testing on a non-localhost host).
	InsecureCookies bool `yaml:"insecure_cookies"`
}

// defaultConfig returns built-in defaults, used as the base before the YAML file
// is decoded on top. Secret/identity fields have no safe default and stay empty
// (validated after load).
func defaultConfig() Config {
	return Config{
		Server: ServerConfig{Addr: ":8080"},
		CORS:   CORSConfig{AllowOrigin: "http://localhost:5173"},
		WorkOS: WorkOSConfig{
			RedirectURI: "http://localhost:8080/auth/callback",
		},
		App: AppConfig{PostLoginURL: "http://localhost:5173"},
	}
}

// loadConfig reads the YAML config file, expands `$VAR`/`${VAR}` env references
// in every string field, and validates required values. The path comes from the
// CONFIG_FILE env var, defaulting to "config.yaml".
func loadConfig() (Config, error) {
	cfg := defaultConfig()

	path := envOr("CONFIG_FILE", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		// Unlike the non-sensitive-only design, the file is now the single
		// source of truth (it carries the secret references), so a missing file
		// is an error rather than "fall back to defaults".
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	if err := cfg.expandEnvRefs(); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// envRefPattern matches a whole-value env reference: `$VAR` or `${VAR}`, where
// VAR is a typical shell-style identifier. We intentionally only expand values
// that are *entirely* a reference (optionally surrounded by whitespace), not
// arbitrary substrings — config values are whole secrets/URLs, not templates.
var envRefPattern = regexp.MustCompile(`^\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?$`)

// expandEnvRefs walks every string field and replaces whole-value env
// references with the variable's value. A reference to an unset (or empty)
// variable is a hard error, naming the field so misconfiguration is obvious.
func (c *Config) expandEnvRefs() error {
	fields := []struct {
		name string
		ptr  *string
	}{
		{"server.addr", &c.Server.Addr},
		{"cors.allow_origin", &c.CORS.AllowOrigin},
		{"workos.client_id", &c.WorkOS.ClientID},
		{"workos.api_key", &c.WorkOS.APIKey},
		{"workos.cookie_password", &c.WorkOS.CookiePassword},
		{"workos.redirect_uri", &c.WorkOS.RedirectURI},
		{"workos.login_provider", &c.WorkOS.LoginProvider},
		{"app.post_login_url", &c.App.PostLoginURL},
	}
	for _, f := range fields {
		expanded, err := expandEnvRef(*f.ptr, f.name)
		if err != nil {
			return err
		}
		*f.ptr = expanded
	}
	return nil
}

// expandEnvRef resolves a single value. If it is an env reference ($VAR/${VAR}),
// it returns the variable's value, erroring if that variable is unset/empty.
// Otherwise it returns the value unchanged.
func expandEnvRef(value, fieldName string) (string, error) {
	m := envRefPattern.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return value, nil // literal value, no reference
	}
	varName := m[1]
	resolved := os.Getenv(varName)
	if resolved == "" {
		return "", fmt.Errorf("config: %s references environment variable %s, which is unset or empty", fieldName, varName)
	}
	return resolved, nil
}

// validate checks that required fields are present and well-formed after
// expansion.
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
