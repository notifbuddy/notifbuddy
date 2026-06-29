package intent

import "testing"

// CloudflareClassifier must satisfy the Classifier interface.
var _ Classifier = (*CloudflareClassifier)(nil)

func TestParseAction(t *testing.T) {
	cases := []struct {
		name     string
		response string
		want     Intent
	}{
		{"strict json create", `{"action":"create-channel"}`, CreateChannel},
		{"strict json close", `{"action":"close-channel"}`, CloseChannel},
		{"strict json no-action", `{"action":"no-action"}`, NoAction},
		{"whitespace padded", "  \n{\"action\":\"create-channel\"}\n ", CreateChannel},
		{"prose prefix", "The answer is {\"action\":\"close-channel\"}.", CloseChannel},
		{"code fence", "```json\n{\"action\":\"create-channel\"}\n```", CreateChannel},
		{"bare command", "close-channel", CloseChannel},
		{"bare command padded", "  create-channel  ", CreateChannel},
		{"unknown action value", `{"action":"frobnicate"}`, NoAction},
		{"empty object", `{}`, NoAction},
		{"not json no command", "I cannot help with that.", NoAction},
		{"truncated json", `{"action":"create`, NoAction},
		{"empty string", "", NoAction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseAction(tc.response); got != tc.want {
				t.Fatalf("parseAction(%q) = %q, want %q", tc.response, got, tc.want)
			}
		})
	}
}

func TestKnown(t *testing.T) {
	for _, s := range []string{"create-channel", "close-channel", "no-action"} {
		if !known(s) {
			t.Errorf("known(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "create", "delete-channel", "CREATE-CHANNEL"} {
		if known(s) {
			t.Errorf("known(%q) = true, want false", s)
		}
	}
}
