package integrations

import "encoding/json"

// maxWebhookBody caps inbound webhook request bodies (generous for provider
// payloads).
const maxWebhookBody = 5 << 20 // 5 MiB

// WebhookEvent is the trimmed view of a stored webhook event for the API.
type WebhookEvent struct {
	DeliveryID string
	EventType  string
	Action     string
	ReceivedAt string
	Payload    json.RawMessage
}
