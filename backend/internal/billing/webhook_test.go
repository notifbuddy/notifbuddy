package billing

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stripe/stripe-go/v86"
)

// event builds a stripe.Event whose data.object is the given JSON.
func event(t *testing.T, objectJSON string) stripe.Event {
	t.Helper()
	return stripe.Event{Data: &stripe.EventData{Raw: json.RawMessage(objectJSON)}}
}

func TestResolveEventOrg(t *testing.T) {
	s := &Service{} // no store: only the metadata/client_reference paths run

	tests := []struct {
		name   string
		object string
		want   string
	}{
		{
			name:   "subscription metadata",
			object: `{"id":"sub_1","metadata":{"org_id":"org_123"}}`,
			want:   "org_123",
		},
		{
			name:   "checkout session client_reference_id",
			object: `{"id":"cs_1","client_reference_id":"org_456","metadata":{}}`,
			want:   "org_456",
		},
		{
			name:   "metadata wins over client_reference_id",
			object: `{"metadata":{"org_id":"org_meta"},"client_reference_id":"org_ref"}`,
			want:   "org_meta",
		},
		{
			name:   "nothing resolvable without a store",
			object: `{"id":"in_1"}`,
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.resolveEventOrg(context.Background(), event(t, tt.object)); got != tt.want {
				t.Errorf("resolveEventOrg = %q, want %q", got, tt.want)
			}
		})
	}
}
