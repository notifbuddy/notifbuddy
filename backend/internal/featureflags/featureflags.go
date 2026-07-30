package featureflags

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Flags struct {
	GitHubOAuthLogin  bool `yaml:"github_oauth_login"`
	DeveloperSettings bool `yaml:"developer_settings"`
}

func Load() (Flags, error) {
	path, err := resolvePath()
	if err != nil {
		return Flags{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Flags{}, fmt.Errorf("featureflags: read %s: %w", path, err)
	}
	var f Flags
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Flags{}, fmt.Errorf("featureflags: parse %s: %w", path, err)
	}
	return f, nil
}

func resolvePath() (string, error) {
	if p := os.Getenv("FEATUREFLAGS_FILE"); p != "" {
		return p, nil
	}
	env := os.Getenv("NB_ENV")
	if env == "" {
		env = "local"
	}
	rel := fmt.Sprintf("config/featureflags/%s.yaml", env)
	for _, prefix := range []string{".", ".."} {
		candidate := prefix + "/" + rel
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("featureflags: %s not found (set FEATUREFLAGS_FILE or NB_ENV)", rel)
}
