package sync

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"xolo/backend/internal/integrations"
	"xolo/backend/internal/template"
)

// linearIssueEventFull builds an issue event with the fields the default topic
// template renders (identifier, title, state, url).
func linearIssueEventFull(issueID, identifier, teamID, stateName, title, url string) json.RawMessage {
	env := map[string]any{"event_source": "linear", "linear": map[string]any{
		"action": "update", "type": "issue", "actor": map[string]any{"name": "Ada"},
		"issue": map[string]any{
			"id": issueID, "identifier": identifier, "teamId": teamID, "title": title, "url": url,
			"state": map[string]any{"name": stateName},
		},
	}}
	b, _ := json.Marshal(env)
	return b
}

func TestOnLinearEvent_CreateSetsTopicBacklink(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["dt1"] = linearIssueEventFull(
		"issue9", "SKO-9", "team1", "In Progress", "Fix login", "https://linear.app/x/issue/SKO-9/fix-login")

	sl := &fakeSlack{nextChannel: "C_T"}
	ig := &fakeIntg{settings: integrations.LinearSettings{
		CreationMode:  "status",
		TriggerStatus: "In Progress",
	}}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("dt1", "org1"))

	want := "C_T|SKO-9: Fix login • In Progress • https://linear.app/x/issue/SKO-9/fix-login"
	if len(sl.topics) != 1 || sl.topics[0] != want {
		t.Fatalf("topics = %v, want [%s]", sl.topics, want)
	}
	if got := st.channelTopics["org1|issue9"]; got != strings.SplitN(want, "|", 2)[1] {
		t.Errorf("stored topic = %q, want the set topic", got)
	}
}

func TestOnLinearEvent_CreateUsesCustomTopicTemplate(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["dt2"] = linearIssueEventFull(
		"issue9", "SKO-9", "team1", "In Progress", "Fix login", "https://linear.app/x/issue/SKO-9/fix-login")

	sl := &fakeSlack{nextChannel: "C_T"}
	ig := &fakeIntg{settings: integrations.LinearSettings{
		CreationMode:  "status",
		TriggerStatus: "In Progress",
		TopicTemplate: "link: ${{ linear.issue.url }}",
	}}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("dt2", "org1"))

	want := "C_T|link: https://linear.app/x/issue/SKO-9/fix-login"
	if len(sl.topics) != 1 || sl.topics[0] != want {
		t.Fatalf("topics = %v, want [%s]", sl.topics, want)
	}
}

func TestOnLinearEvent_CreatePostsTicketBodyIntro(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["dt6"] = linearIssueEventFull(
		"issue9", "SKO-9", "team1", "In Progress", "Fix login", "https://linear.app/x/issue/SKO-9/fix-login")

	sl := &fakeSlack{nextChannel: "C_T"}
	ig := &fakeIntg{
		settings: integrations.LinearSettings{CreationMode: "status", TriggerStatus: "In Progress"},
		issue: integrations.LinearIssue{
			ID: "issue9", Identifier: "SKO-9", Title: "Fix login",
			Description: "Users cannot log in with SSO.",
			URL:         "https://linear.app/x/issue/SKO-9/fix-login",
		},
	}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("dt6", "org1"))

	if len(sl.posted) != 1 {
		t.Fatalf("want 1 intro post, got %d", len(sl.posted))
	}
	intro := sl.posted[0]
	if intro.ChannelID != "C_T" {
		t.Errorf("intro channel = %q, want C_T", intro.ChannelID)
	}
	for _, want := range []string{"SKO-9", "Fix login", "Users cannot log in with SSO.", "https://linear.app/x/issue/SKO-9/fix-login"} {
		if !strings.Contains(intro.Text, want) {
			t.Errorf("intro text %q missing %q", intro.Text, want)
		}
	}
}

func TestOnLinearEvent_IssueUpdateSyncsTopic(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["dt3"] = linearIssueEventFull(
		"issue9", "SKO-9", "team1", "In Review", "Fix login", "https://linear.app/x/issue/SKO-9/fix-login")
	st.issueToChannel["org1|issue9"] = "C_LIVE"
	st.channelToIssue["org1|C_LIVE"] = "issue9"
	st.channelTopics["org1|issue9"] = "SKO-9: Fix login • In Progress • https://linear.app/x/issue/SKO-9/fix-login"

	sl := &fakeSlack{}
	ig := &fakeIntg{settings: integrations.LinearSettings{CreationMode: "manual"}}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("dt3", "org1"))

	want := "C_LIVE|SKO-9: Fix login • In Review • https://linear.app/x/issue/SKO-9/fix-login"
	if len(sl.topics) != 1 || sl.topics[0] != want {
		t.Fatalf("topics = %v, want [%s]", sl.topics, want)
	}
	if got := st.channelTopics["org1|issue9"]; !strings.Contains(got, "In Review") {
		t.Errorf("stored topic = %q, want updated status", got)
	}
	if sl.createdName != "" || sl.archivedChannel != "" {
		t.Errorf("topic sync must not create/archive; created=%q archived=%q", sl.createdName, sl.archivedChannel)
	}
}

func TestOnLinearEvent_IssueUpdateTopicUnchangedSkipsSlack(t *testing.T) {
	topic := "SKO-9: Fix login • In Progress • https://linear.app/x/issue/SKO-9/fix-login"
	st := newFakeStore()
	st.linearPayloads["dt4"] = linearIssueEventFull(
		"issue9", "SKO-9", "team1", "In Progress", "Fix login", "https://linear.app/x/issue/SKO-9/fix-login")
	st.issueToChannel["org1|issue9"] = "C_LIVE"
	st.channelToIssue["org1|C_LIVE"] = "issue9"
	st.channelTopics["org1|issue9"] = topic

	sl := &fakeSlack{}
	ig := &fakeIntg{settings: integrations.LinearSettings{CreationMode: "manual"}}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("dt4", "org1"))

	if len(sl.topics) != 0 {
		t.Fatalf("unchanged topic must not call Slack; got %v", sl.topics)
	}
}

func TestOnLinearEvent_TopicSetFailureKeepsStoredTopic(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["dt5"] = linearIssueEventFull(
		"issue9", "SKO-9", "team1", "In Review", "Fix login", "https://linear.app/x/issue/SKO-9/fix-login")
	st.issueToChannel["org1|issue9"] = "C_LIVE"
	st.channelToIssue["org1|C_LIVE"] = "issue9"
	st.channelTopics["org1|issue9"] = "old topic"

	sl := &fakeSlack{topicErr: errors.New("ratelimited")}
	ig := &fakeIntg{settings: integrations.LinearSettings{CreationMode: "manual"}}
	e := newEngine(st, sl, ig, &spyPub{})

	if err := e.OnLinearEvent(context.Background(), linearRef("dt5", "org1")); err != nil {
		t.Fatalf("topic failure is best-effort, must ack; got %v", err)
	}
	if got := st.channelTopics["org1|issue9"]; got != "old topic" {
		t.Errorf("stored topic = %q, want unchanged on Slack failure", got)
	}
}

func TestChannelTopicTruncates(t *testing.T) {
	e := newEngine(newFakeStore(), &fakeSlack{}, &fakeIntg{}, &spyPub{})
	var p linearPayload
	if err := json.Unmarshal(linearIssueEventFull(
		"i", "SKO-1", "t", "Todo", strings.Repeat("x", 400), "https://linear.app/x"), &p); err != nil {
		t.Fatal(err)
	}
	evt := template.Event{EventType: "linear", Linear: envelopeLinear(p)}
	topic := e.channelTopic(context.Background(), integrations.LinearSettings{}, evt)
	if got := len([]rune(topic)); got != slackTopicMaxLen {
		t.Errorf("topic length = %d runes, want %d", got, slackTopicMaxLen)
	}
	if !strings.HasSuffix(topic, "…") {
		t.Errorf("truncated topic should end with ellipsis; got %q", topic[len(topic)-9:])
	}
}
