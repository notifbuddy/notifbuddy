package tokenstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"

	"xolo/cli/internal/config"
)

const service = "notifbuddy-cli"

var ErrNotLoggedIn = errors.New("not logged in — run `notifbuddy login`")

type fileTokens map[string]string

func fallbackPath() string { return filepath.Join(config.Dir(), "tokens.json") }

func Save(authURL, token string) error {
	if err := keyring.Set(service, authURL, token); err == nil {
		return nil
	}
	tokens := readFile()
	tokens[authURL] = token
	return writeFile(tokens)
}

func Load(authURL string) (string, error) {
	if t, err := keyring.Get(service, authURL); err == nil && t != "" {
		return t, nil
	}
	if t := readFile()[authURL]; t != "" {
		return t, nil
	}
	return "", ErrNotLoggedIn
}

func Clear(authURL string) error {
	kerr := keyring.Delete(service, authURL)
	tokens := readFile()
	if _, ok := tokens[authURL]; ok {
		delete(tokens, authURL)
		if err := writeFile(tokens); err != nil {
			return err
		}
		return nil
	}
	if kerr != nil && !errors.Is(kerr, keyring.ErrNotFound) {
		return fmt.Errorf("clear token: %w", kerr)
	}
	return nil
}

func readFile() fileTokens {
	tokens := fileTokens{}
	raw, err := os.ReadFile(fallbackPath())
	if err != nil {
		return tokens
	}
	_ = json.Unmarshal(raw, &tokens)
	return tokens
}

func writeFile(tokens fileTokens) error {
	if err := os.MkdirAll(config.Dir(), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	return os.WriteFile(fallbackPath(), raw, 0o600)
}
