package sync

import "testing"

func TestEmojiToLinear(t *testing.T) {
	u, ok := emojiToLinear("thumbsup")
	if !ok || u != "👍" {
		t.Fatalf("thumbsup = %q %v", u, ok)
	}
	u, ok = emojiToLinear("+1")
	if !ok || u != "👍" {
		t.Fatalf("+1 = %q %v", u, ok)
	}
	if _, ok := emojiToLinear("party_parrot"); ok {
		t.Fatal("custom emoji must be unmapped")
	}
}

func TestEmojiToSlack(t *testing.T) {
	// gemoji's primary alias for 👍 is "+1" (also aliased as thumbsup).
	name, ok := emojiToSlack("👍")
	if !ok || (name != "+1" && name != "thumbsup") {
		t.Fatalf("👍 = %q %v", name, ok)
	}
	name, ok = emojiToSlack("+1")
	if !ok || name != "+1" {
		t.Fatalf("shortcode +1 = %q %v", name, ok)
	}
	// Private-use codepoint — not in gemoji.
	if _, ok := emojiToSlack("\uE000"); ok {
		t.Fatal("unknown unicode must be unmapped")
	}
}
