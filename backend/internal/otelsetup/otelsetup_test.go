package otelsetup_test

import (
	"context"
	"testing"

	"xolo/backend/internal/config"
	"xolo/backend/internal/otelsetup"
)

func TestSetupDisabled(t *testing.T) {
	shutdown, err := otelsetup.Setup(context.Background(), config.OTelConfig{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
