package config

import (
	"strings"
	"testing"
)

// validCfg returns a config that passes the non-pubsub validation rules.
func validCfg() Config {
	return defaultConfig() // auth.base_url has a dev default; nothing else is required
}

func TestValidate_PubSubProvider(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*Config)
		wantErr string // substring; "" = valid
	}{
		{"empty defaults to postgres", func(c *Config) {}, ""},
		{"postgres", func(c *Config) { c.PubSub.Provider = "postgres" }, ""},
		{"postgres with poll interval", func(c *Config) {
			c.PubSub.Provider = "postgres"
			c.PubSub.Postgres.PollInterval = "200ms"
		}, ""},
		{"postgres bad poll interval", func(c *Config) {
			c.PubSub.Postgres.PollInterval = "fast"
		}, "poll_interval"},
		{"postgres negative poll interval", func(c *Config) {
			c.PubSub.Postgres.PollInterval = "-1s"
		}, "must be positive"},
		{"gcp fully configured", func(c *Config) {
			c.PubSub.Provider = "gcp"
			c.PubSub.GCP = GCPPubSub{
				ProjectID:          "p",
				PushAudience:       "https://api.example.com/internal/pubsub/push",
				PushServiceAccount: "push@p.iam.gserviceaccount.com",
			}
		}, ""},
		{"gcp missing fields", func(c *Config) {
			c.PubSub.Provider = "gcp"
			c.PubSub.GCP = GCPPubSub{ProjectID: "p"}
		}, "pubsub.provider=gcp requires"},
		{"unknown provider", func(c *Config) { c.PubSub.Provider = "sqs" }, "unknown pubsub.provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validCfg()
			tc.mutate(&cfg)
			err := cfg.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestPollIntervalDuration_Default(t *testing.T) {
	if d := (PostgresPubSub{}).PollIntervalDuration(); d.Seconds() != 1 {
		t.Fatalf("default poll interval = %s, want 1s", d)
	}
}
func TestValidate_WebhookSecretsRequiredWhenConfigured(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*Config)
		wantErr string // substring; "" = valid
	}{
		{"slack unconfigured is fine", func(c *Config) {}, ""},
		{"slack configured with signing secret", func(c *Config) {
			c.Slack.ClientSecret = "s"
			c.Slack.SigningSecret = "sig"
		}, ""},
		{"slack configured without signing secret", func(c *Config) {
			c.Slack.ClientSecret = "s"
		}, "slack.signing_secret is required"},
		{"linear configured with webhook secret", func(c *Config) {
			c.Linear.ClientSecret = "s"
			c.Linear.WebhookSecret = "wh"
		}, ""},
		{"linear configured without webhook secret", func(c *Config) {
			c.Linear.ClientSecret = "s"
		}, "linear.webhook_secret is required"},
		{"axiom disabled ignores token fields", func(c *Config) {}, ""},
		{"axiom enabled with token and dataset", func(c *Config) {
			c.Logging.AxiomEnabled = true
			c.Logging.AxiomToken = "xapt-test"
			c.Logging.AxiomDataset = "backend"
		}, ""},
		{"axiom enabled without token fails", func(c *Config) {
			c.Logging.AxiomEnabled = true
			c.Logging.AxiomDataset = "backend"
		}, "logging.axiom_enabled is true but"},
		{"axiom enabled without dataset fails", func(c *Config) {
			c.Logging.AxiomEnabled = true
			c.Logging.AxiomToken = "xapt-test"
		}, "logging.axiom_enabled is true but"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validCfg()
			tc.mutate(&cfg)
			err := cfg.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}
