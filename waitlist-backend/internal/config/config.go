// Package config loads the waitlist service's configuration from a YAML file,
// with secret fields referencing environment variables — the same convention
// as the main backend's config package, trimmed to this service's three
// sections.
//
// Any string value of the form `$VAR` or `${VAR}` is expanded from the
// environment at load time. Values without a `$` are used literally.
package config

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the full service configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	CORS     CORSConfig     `yaml:"cors"`
	Database DatabaseConfig `yaml:"database"`
}

type ServerConfig struct {
	// Addr is the listen address.
	Addr string `yaml:"addr"`
}

type CORSConfig struct {
	// AllowOrigin is the exact origin permitted to call the API.
	AllowOrigin string `yaml:"allow_origin"`
}

type DatabaseConfig struct {
	// URL is the Postgres connection string (the Neon `notifbuddy-waitlist`
	// database). SECRET — set to an env ref.
	URL string `yaml:"url"`
}

func defaultConfig() Config {
	return Config{Server: ServerConfig{Addr: ":8081"}}
}

// Load reads CONFIG_FILE (default config.local.yaml), expands env references,
// and validates required fields.
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
// value.
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
// it returns the variable's value, or empty if that variable is unset.
// Otherwise it returns the value unchanged. Required fields are enforced
// afterwards by validate().
func expandEnvRef(value, fieldName string) (string, error) {
	m := envRefPattern.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return value, nil
	}
	return os.Getenv(m[1]), nil
}

// validate checks required fields after expansion. Unlike the main backend,
// this service is nothing without its database, so the URL is mandatory.
func (c *Config) validate() error {
	if c.Database.URL == "" {
		return fmt.Errorf("database.url is required (e.g. set it to $WAITLIST_DATABASE_URL)")
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
