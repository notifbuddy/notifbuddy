package sync

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// botIdentity is our bot's per-provider identity for an org. Fields are
// Slack-specific today (auth.test + users.info); other providers can land
// alongside later without renaming these.
type botIdentity struct {
	SlackUserID      string
	SlackDisplayName string
}

// botMentioned reports whether body addresses our bot: a Slack user mention of
// botUserID (<@U…> / <@U…|label>), or a case-insensitive text match on the
// resolved display name (Linear comments, plain text).
func botMentioned(body, botUserID, displayName string) bool {
	if botUserID != "" {
		if strings.Contains(body, "<@"+botUserID+">") || strings.Contains(body, "<@"+botUserID+"|") {
			return true
		}
	}
	if displayName != "" {
		if strings.Contains(strings.ToLower(body), strings.ToLower(displayName)) {
			return true
		}
	}
	return false
}

// slackUserMentionRE matches Slack user/bot mentions: <@U123> or <@U123|label>.
var slackUserMentionRE = regexp.MustCompile(`<@([^|>]+)(?:\|([^>]*))?>`)

// rewriteSlackMentions replaces every <@U…> / <@U…|label> in body for Linear:
//  1. Linked Linear identity → profile URL (Linear turns it into an @mention)
//  2. Else Slack display name via users.info → @Name
//  3. Else embedded |label → @label; else leave markup unchanged
//
// Slack users.info results are cached inside the slackapi client.
func (e *Engine) rewriteSlackMentions(ctx context.Context, orgID, token, body string) string {
	if body == "" || !strings.Contains(body, "<@") {
		return body
	}
	return slackUserMentionRE.ReplaceAllStringFunc(body, func(m string) string {
		sub := slackUserMentionRE.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		userID, label := sub[1], ""
		if len(sub) > 2 {
			label = sub[2]
		}
		if mention, ok := e.intg.LinearMentionForSlackUser(ctx, orgID, userID); ok && mention != "" {
			return mention
		}
		if u, err := e.slack.UserByID(ctx, token, userID); err == nil {
			if name := strings.TrimSpace(u.Name); name != "" {
				return "@" + name
			}
		}
		if label != "" {
			return "@" + label
		}
		return m
	})
}

// resolveBotIdentity loads our Slack bot user id and display name for orgID.
// Fail closed: any error or empty field logs a warn and returns ok=false so
// callers skip NLP even if the body contains a product name string.
// users.info / auth.test caching lives in the Slack client, not on the engine.
func (e *Engine) resolveBotIdentity(ctx context.Context, orgID, token string) (botIdentity, bool) {
	userID, err := e.slack.AuthTestUserID(ctx, token)
	if err != nil || userID == "" {
		slog.WarnContext(ctx, "sync: bot identity: auth.test failed; skipping NLP",
			"org_id", orgID, "error", errOrEmpty(err, "empty user_id"))
		return botIdentity{}, false
	}
	u, err := e.slack.UserByID(ctx, token, userID)
	if err != nil || strings.TrimSpace(u.Name) == "" {
		slog.WarnContext(ctx, "sync: bot identity: users.info failed; skipping NLP",
			"org_id", orgID, "bot_user_id", userID, "error", errOrEmpty(err, "empty display name"))
		return botIdentity{}, false
	}
	return botIdentity{SlackUserID: userID, SlackDisplayName: strings.TrimSpace(u.Name)}, true
}

func errOrEmpty(err error, emptyMsg string) any {
	if err != nil {
		return err
	}
	return emptyMsg
}
