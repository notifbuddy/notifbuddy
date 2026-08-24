package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

const (
	DefaultAPIURL       = "https://api.notifbuddy.com"
	DefaultAuthURL      = "https://auth.notifbuddy.com"
	DefaultDashboardURL = "https://dashboard.notifbuddy.com"
)

func Dir() string {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "notifbuddy")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".notifbuddy"
	}
	return filepath.Join(home, ".config", "notifbuddy")
}

const (
	LocalAPIURL       = "http://localhost:8080"
	LocalAuthURL      = "http://localhost:8787"
	LocalDashboardURL = "http://localhost:5173"
)

func loadDotenv() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = godotenv.Load(filepath.Join(filepath.Dir(exe), ".env"))
}

func Init(v *viper.Viper, explicitFile string) error {
	loadDotenv()

	apiURL, authURL, dashURL := DefaultAPIURL, DefaultAuthURL, DefaultDashboardURL
	if os.Getenv("NOTIFBUDDY_ENV") == "local" {
		apiURL, authURL, dashURL = LocalAPIURL, LocalAuthURL, LocalDashboardURL
	}
	v.SetDefault("api_url", apiURL)
	v.SetDefault("auth_url", authURL)
	v.SetDefault("dashboard_url", dashURL)

	v.SetEnvPrefix("NOTIFBUDDY")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	if explicitFile != "" {
		v.SetConfigFile(explicitFile)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(Dir())
	}
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if explicitFile == "" && (errorsAs(err, &notFound) || os.IsNotExist(err)) {
			return nil
		}
		if explicitFile == "" {
			if _, statErr := os.Stat(filepath.Join(Dir(), "config.yaml")); os.IsNotExist(statErr) {
				return nil
			}
		}
		return err
	}
	return nil
}

func errorsAs(err error, target *viper.ConfigFileNotFoundError) bool {
	if e, ok := err.(viper.ConfigFileNotFoundError); ok {
		*target = e
		return true
	}
	return false
}

func Trim(u string) string { return strings.TrimRight(u, "/") }
