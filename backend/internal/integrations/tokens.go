package integrations

import (
	"encoding/json"
	"fmt"
	"time"
)

type tokenBundle struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func marshalTokenBundle(b tokenBundle) ([]byte, error) {
	if b.AccessToken == "" {
		return nil, fmt.Errorf("integrations: empty access token")
	}
	return json.Marshal(b)
}

func parseTokenBundle(plaintext []byte) tokenBundle {
	var b tokenBundle
	if err := json.Unmarshal(plaintext, &b); err == nil && b.AccessToken != "" {
		return b
	}
	return tokenBundle{AccessToken: string(plaintext)}
}

func expiresWithin(expiresAt *time.Time, skew time.Duration, now time.Time) bool {
	if expiresAt == nil {
		return false
	}
	return !expiresAt.After(now.Add(skew))
}

func expiryFromExpiresIn(expiresIn int, now time.Time) *time.Time {
	if expiresIn <= 0 {
		return nil
	}
	t := now.Add(time.Duration(expiresIn) * time.Second)
	return &t
}
