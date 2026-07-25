package sync

import (
	"context"
	"testing"

	"xolo/backend/internal/slackapi"
)

func TestBotMentioned(t *testing.T) {
	tests := []struct {
		name, body, botID, display string
		want                       bool
	}{
		{"slack id", "<@U_BOT> close", "U_BOT", "notifbuddy", true},
		{"slack id with label", "<@U_BOT|notifbuddy> close", "U_BOT", "x", true},
		{"wrong id", "<@U_OTHER> close", "U_BOT", "notifbuddy", false},
		{"display name", "@my-bot create", "U_BOT", "my-bot", true},
		{"display case", "@My-Bot archive", "U_BOT", "my-bot", true},
		{"no match", "hello world", "U_BOT", "notifbuddy", false},
		{"empty identity", "@notifbuddy create", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := botMentioned(tt.body, tt.botID, tt.display); got != tt.want {
				t.Errorf("botMentioned(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestRewriteSlackMentions(t *testing.T) {
	sl := &fakeSlack{usersByID: map[string]slackapi.User{
		"U_BOT":   {ID: "U_BOT", Name: "my-bot"},
		"U_OTHER": {ID: "U_OTHER", Name: "parrot"},
		"U_LINK":  {ID: "U_LINK", Name: "ada-slack"},
	}}
	ig := &fakeIntg{linearMentions: map[string]string{
		"U_LINK": "https://linear.app/acme/profiles/ada",
	}}
	e := newEngine(newFakeStore(), sl, ig, &spyPub{})

	got := e.rewriteSlackMentions(context.Background(), "org1", "xoxb",
		"<@U_BOT> thanks <@U_OTHER> and <@U_LINK> plus <@U_GONE|fallback>")
	want := "@my-bot thanks @parrot and https://linear.app/acme/profiles/ada plus @fallback"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
