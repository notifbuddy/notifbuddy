package main

import (
	"testing"

	"xolo/backend/internal/integrations"
	"xolo/backend/internal/pubsub"
	syncengine "xolo/backend/internal/sync"
)

// TestPublishedTopicsAreInManifest pins every topic constant the code
// publishes to to the shared topology file (internal/pubsub/manifest.yaml,
// also read by infra). A constant missing from the manifest would
// publish to a topic that GCP never had created — this fails first.
func TestPublishedTopicsAreInManifest(t *testing.T) {
	manifest, err := pubsub.Topics()
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	have := map[string]bool{}
	for _, topic := range manifest {
		have[topic] = true
	}

	published := []string{
		integrations.GitHubWebhookReceivedTopic,
		integrations.LinearWebhookReceivedTopic,
		integrations.SlackWebhookReceivedTopic,
		integrations.GitHubWebhookTopic,
		integrations.LinearWebhookTopic,
		integrations.SlackWebhookTopic,
		pubsub.PoisonTopic,
	}
	published = append(published, syncengine.AllTopics...)

	for _, topic := range published {
		if !have[topic] {
			t.Errorf("code publishes to %q but internal/pubsub/manifest.yaml does not list it", topic)
		}
	}
	if len(have) != len(published) {
		t.Errorf("manifest lists %d topics, code knows %d — remove stale manifest entries or add the constant here", len(have), len(published))
	}
}