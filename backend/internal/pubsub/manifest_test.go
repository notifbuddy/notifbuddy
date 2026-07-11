package pubsub

import (
	"context"
	"strings"
	"testing"
)

func nopHandler(context.Context, Message) error { return nil }

// manifestHandlers returns a handler for every subscription in the manifest.
func manifestHandlers(t *testing.T) map[string]Handler {
	t.Helper()
	m, err := loadManifest()
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	handlers := map[string]Handler{}
	for name := range m.Subscriptions {
		handlers[name] = nopHandler
	}
	return handlers
}

func TestBindSubscriptions_FullBinding(t *testing.T) {
	subs, err := BindSubscriptions(manifestHandlers(t))
	if err != nil {
		t.Fatalf("BindSubscriptions: %v", err)
	}
	for _, sub := range subs {
		if sub.Topic == "" || sub.Group == "" || sub.Handle == nil {
			t.Errorf("subscription %q incompletely bound: %+v", sub.Name, sub)
		}
	}
}

func TestBindSubscriptions_MissingHandlerFails(t *testing.T) {
	handlers := manifestHandlers(t)
	for name := range handlers {
		delete(handlers, name)
		break
	}
	if _, err := BindSubscriptions(handlers); err == nil || !strings.Contains(err.Error(), "no handler bound") {
		t.Fatalf("BindSubscriptions = %v, want missing-handler error", err)
	}
}

func TestBindSubscriptions_UnknownHandlerFails(t *testing.T) {
	handlers := manifestHandlers(t)
	handlers["not-in-manifest"] = nopHandler
	if _, err := BindSubscriptions(handlers); err == nil || !strings.Contains(err.Error(), "not declared in manifest") {
		t.Fatalf("BindSubscriptions = %v, want undeclared-handler error", err)
	}
}