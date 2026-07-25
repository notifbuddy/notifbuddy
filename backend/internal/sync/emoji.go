package sync

import (
	"strings"

	"github.com/kenshaw/emoji"
)

// emojiToLinear returns the Unicode form for a Slack shortcode (no colons), or
// ("", false) when the name is custom/unmapped. Mapping comes from gemoji via
// github.com/notifbuddy/emoji (pinned with a go.mod replace).
func emojiToLinear(slackName string) (string, bool) {
	e := emoji.FromAlias(slackName)
	if e == nil || e.Emoji == "" {
		return "", false
	}
	return e.Emoji, true
}

// emojiToSlack returns a Slack shortcode for a Linear reaction emoji (Unicode
// or shortcode), or ("", false) when unmapped. Prefers the first gemoji alias.
func emojiToSlack(linearEmoji string) (string, bool) {
	if name, ok := aliasForCode(linearEmoji); ok {
		return name, true
	}
	// Linear may echo a shortcode/name instead of Unicode.
	if e := emoji.FromAlias(linearEmoji); e != nil && len(e.Aliases) > 0 {
		return e.Aliases[0], true
	}
	// Some payloads include U+FE0F (emoji presentation); retry without it.
	trimmed := strings.TrimSuffix(linearEmoji, "\uFE0F")
	if trimmed != linearEmoji {
		return aliasForCode(trimmed)
	}
	return "", false
}

func aliasForCode(code string) (string, bool) {
	e := emoji.FromCode(code)
	if e == nil || len(e.Aliases) == 0 {
		return "", false
	}
	return e.Aliases[0], true
}
