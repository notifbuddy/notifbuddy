package featureflags_test

import (
	"os"
	"path/filepath"
	"testing"

	"xolo/backend/internal/featureflags"
)

func TestLoad_DeveloperSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flags.yaml")
	if err := os.WriteFile(path, []byte("github_oauth_login: true\ndeveloper_settings: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FEATUREFLAGS_FILE", path)

	f, err := featureflags.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !f.DeveloperSettings {
		t.Fatal("want developer_settings true")
	}
}

func TestLoad_DeveloperSettingsDefaultsFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flags.yaml")
	if err := os.WriteFile(path, []byte("github_oauth_login: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FEATUREFLAGS_FILE", path)

	f, err := featureflags.Load()
	if err != nil {
		t.Fatal(err)
	}
	if f.DeveloperSettings {
		t.Fatal("missing developer_settings must default false")
	}
}
