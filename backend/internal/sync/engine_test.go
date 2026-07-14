package sync

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"xolo/backend/internal/integrations"
	"xolo/backend/internal/pubsub"
	"xolo/backend/internal/slackapi"
	"xolo/backend/internal/store"
)

// --- fakes ------------------------------------------------------------------

// fakeStore is an in-memory Store for engine tests. Payloads are keyed by id;
// the routing tables are plain maps.
type fakeStore struct {
	linearPayloads map[string]json.RawMessage
	slackPayloads  map[string]json.RawMessage
	issueToChannel map[string]string // key: org|issue
	channelToIssue map[string]string // key: org|channel
	linksBySlack   map[string]store.MirroredMessage
	linksByLinear  map[string]store.MirroredMessage
	recorded       []store.MirroredMessage
	deletedIssues  []string
	statePatches   []statePatch
}

// statePatch records a PatchLinearTeamState call for assertions.
type statePatch struct {
	teamID  string
	state   store.LinearWorkflowState
	removed bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		linearPayloads: map[string]json.RawMessage{},
		slackPayloads:  map[string]json.RawMessage{},
		issueToChannel: map[string]string{},
		channelToIssue: map[string]string{},
		linksBySlack:   map[string]store.MirroredMessage{},
		linksByLinear:  map[string]store.MirroredMessage{},
	}
}

func (f *fakeStore) LinearWebhookPayload(_ context.Context, id string) (json.RawMessage, error) {
	if p, ok := f.linearPayloads[id]; ok {
		return p, nil
	}
	return nil, store.ErrNotFound
}
func (f *fakeStore) SlackWebhookPayload(_ context.Context, id string) (json.RawMessage, error) {
	if p, ok := f.slackPayloads[id]; ok {
		return p, nil
	}
	return nil, store.ErrNotFound
}
func (f *fakeStore) LockIssue(_ context.Context, _, _ string) (func(), error) {
	return func() {}, nil
}
func (f *fakeStore) UpsertIssueChannel(_ context.Context, in store.IssueChannel) error {
	f.issueToChannel[in.OrgID+"|"+in.LinearIssueID] = in.SlackChannelID
	f.channelToIssue[in.OrgID+"|"+in.SlackChannelID] = in.LinearIssueID
	return nil
}
func (f *fakeStore) ChannelForIssue(_ context.Context, org, issue string) (string, error) {
	if c, ok := f.issueToChannel[org+"|"+issue]; ok {
		return c, nil
	}
	return "", store.ErrNotFound
}
func (f *fakeStore) IssueForChannel(_ context.Context, org, channel string) (string, error) {
	if i, ok := f.channelToIssue[org+"|"+channel]; ok {
		return i, nil
	}
	return "", store.ErrNotFound
}
func (f *fakeStore) DeleteIssueChannel(_ context.Context, org, issue string) error {
	f.deletedIssues = append(f.deletedIssues, issue)
	if c, ok := f.issueToChannel[org+"|"+issue]; ok {
		delete(f.channelToIssue, org+"|"+c)
	}
	delete(f.issueToChannel, org+"|"+issue)
	return nil
}
func (f *fakeStore) RecordMirroredMessage(_ context.Context, m store.MirroredMessage) error {
	f.recorded = append(f.recorded, m)
	f.linksByLinear[m.OrgID+"|"+m.LinearCommentID] = m
	f.linksBySlack[m.OrgID+"|"+m.SlackChannelID+"|"+m.SlackTS] = m
	return nil
}
func (f *fakeStore) LinkBySlackTS(_ context.Context, org, channel, ts string) (store.MirroredMessage, error) {
	if m, ok := f.linksBySlack[org+"|"+channel+"|"+ts]; ok {
		return m, nil
	}
	return store.MirroredMessage{}, store.ErrNotFound
}
func (f *fakeStore) LinkByLinearComment(_ context.Context, org, id string) (store.MirroredMessage, error) {
	if m, ok := f.linksByLinear[org+"|"+id]; ok {
		return m, nil
	}
	return store.MirroredMessage{}, store.ErrNotFound
}
func (f *fakeStore) PatchLinearTeamState(_ context.Context, _ string, teamID string, st store.LinearWorkflowState, removed bool) error {
	f.statePatches = append(f.statePatches, statePatch{teamID: teamID, state: st, removed: removed})
	return nil
}

// fakeSlack records calls and returns canned ids.
type fakeSlack struct {
	posted          []slackapi.PostOptions
	createdName     string
	archivedChannel string
	invited         []string
	nextTS          string
	nextChannel     string
	botUserID       string
	usersByEmail    map[string]slackapi.User
	usersByID       map[string]slackapi.User
}

func (s *fakeSlack) CreateChannel(_ context.Context, _, name string) (string, error) {
	s.createdName = name
	if s.nextChannel == "" {
		s.nextChannel = "C_NEW"
	}
	return s.nextChannel, nil
}
func (s *fakeSlack) ArchiveChannel(_ context.Context, _, channelID string) error {
	s.archivedChannel = channelID
	return nil
}
func (s *fakeSlack) DeleteChannel(_ context.Context, _, _ string) error  { return nil }
func (s *fakeSlack) InviteUsers(_ context.Context, _, _ string, ids []string) error {
	s.invited = append(s.invited, ids...)
	return nil
}
func (s *fakeSlack) PostMessage(_ context.Context, _ string, opts slackapi.PostOptions) (string, error) {
	s.posted = append(s.posted, opts)
	if s.nextTS == "" {
		return "1700000000.000001", nil
	}
	return s.nextTS, nil
}
func (s *fakeSlack) LookupUserByEmail(_ context.Context, _, email string) (slackapi.User, error) {
	if u, ok := s.usersByEmail[email]; ok {
		return u, nil
	}
	return slackapi.User{}, store.ErrNotFound
}
func (s *fakeSlack) UserByID(_ context.Context, _, id string) (slackapi.User, error) {
	if u, ok := s.usersByID[id]; ok {
		return u, nil
	}
	return slackapi.User{}, store.ErrNotFound
}
func (s *fakeSlack) AuthTestUserID(_ context.Context, _ string) (string, error) {
	return s.botUserID, nil
}

// fakeIntg satisfies Integrations.
//
// settings is the config returned for any mapped team; teamMapped controls which
// teams resolve to it (nil → every team maps to settings, matching the old
// single-config behavior so existing tests keep passing). issueTeamID is what
// LinearIssueByID reports (used by the @notifbuddy path to resolve the team).
type fakeIntg struct {
	settings        integrations.LinearSettings
	teamMapped      map[string]bool
	issueTeamID     string
	createdComments []integrations.LinearCreateCommentInput
	nextCommentID   string
}

func (i *fakeIntg) SlackBotToken(context.Context, string) (string, error) { return "xoxb-test", nil }
func (i *fakeIntg) LinearCreateComment(_ context.Context, _ string, in integrations.LinearCreateCommentInput) (integrations.LinearComment, error) {
	i.createdComments = append(i.createdComments, in)
	id := i.nextCommentID
	if id == "" {
		id = "cmt_new"
	}
	return integrations.LinearComment{ID: id}, nil
}
func (i *fakeIntg) LinearIssueByID(context.Context, string, string) (integrations.LinearIssue, error) {
	return integrations.LinearIssue{TeamID: i.issueTeamID}, nil
}
func (i *fakeIntg) SettingForTeam(_ context.Context, _ string, teamID string) (integrations.LinearSettings, error) {
	// nil map → any team maps (legacy single-config tests). Otherwise only teams
	// explicitly marked true resolve; the rest are unmapped (ErrNotFound).
	if i.teamMapped == nil || i.teamMapped[teamID] {
		return i.settings, nil
	}
	return integrations.LinearSettings{}, store.ErrNotFound
}

// spyPub records published topics.
type spyPub struct{ topics []string }

func (p *spyPub) Publish(_ context.Context, m pubsub.Message) error {
	p.topics = append(p.topics, m.Topic)
	return nil
}
func (p *spyPub) has(topic string) bool {
	return slices.Contains(p.topics, topic)
}

// newEngine builds an engine over the fakes.
func newEngine(st Store, sl SlackActions, ig Integrations, pub pubsub.Publisher) *Engine {
	return New(st, sl, ig, nil, pub, nil)
}

// --- helpers ----------------------------------------------------------------

func linearCommentPayload(action, commentID, body, issueID, parentID, actorName, actorEmail string, botActor bool) json.RawMessage {
	data := map[string]any{
		"id":      commentID,
		"body":    body,
		"issueId": issueID,
	}
	if parentID != "" {
		data["parentId"] = parentID
	}
	if botActor {
		data["botActor"] = map[string]any{"id": "app_1", "name": "NotifBuddy"}
	}
	env := map[string]any{
		"event_type": "linear",
		"linear": map[string]any{
			"action": action,
			"type":   "Comment",
			"actor":  map[string]any{"name": actorName, "email": actorEmail, "type": "user"},
			"data":   data,
		},
	}
	b, _ := json.Marshal(env)
	return b
}

func slackMessagePayload(user, botID, subtype, text, channel, ts, threadTS string) json.RawMessage {
	ev := map[string]any{"type": "message", "user": user, "text": text, "channel": channel, "ts": ts}
	if botID != "" {
		ev["bot_id"] = botID
	}
	if subtype != "" {
		ev["subtype"] = subtype
	}
	if threadTS != "" {
		ev["thread_ts"] = threadTS
	}
	b, _ := json.Marshal(map[string]any{"event": ev})
	return b
}

func linearRef(deliveryID, orgID string) pubsub.Message {
	b, _ := json.Marshal(linearEventRef{DeliveryID: deliveryID, OrgID: orgID})
	return pubsub.Message{Topic: "integrations.linear.webhook_event", Payload: b}
}
func slackRef(eventID, orgID string) pubsub.Message {
	b, _ := json.Marshal(slackEventRef{EventID: eventID, OrgID: orgID})
	return pubsub.Message{Topic: "integrations.slack.webhook_event", Payload: b}
}

// --- tests ------------------------------------------------------------------

// Defense 1: a Linear comment our own app authored (botActor present) must NOT
// be mirrored back into Slack — that is what breaks the loop.
func TestOnLinearEvent_DropsAppAuthoredComment(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["d1"] = linearCommentPayload("create", "c1", "echo", "issue1", "", "Ada", "ada@x.io", true /*botActor*/)
	st.issueToChannel["org1|issue1"] = "C1" // channel exists, so only Defense 1 stops it
	st.channelToIssue["org1|C1"] = "issue1"

	sl := &fakeSlack{}
	pub := &spyPub{}
	e := newEngine(st, sl, &fakeIntg{}, pub)

	e.OnLinearEvent(context.Background(), linearRef("d1", "org1"))

	if len(sl.posted) != 0 {
		t.Fatalf("app-authored comment must not post to Slack; got %d posts", len(sl.posted))
	}
	if pub.has(TopicSlackMessage) {
		t.Error("no sync.slack.message.posted should fire for a dropped echo")
	}
}

// A human Linear comment on an issue with a channel mirrors into Slack, posting
// as the bot but with the author's name/avatar (attribution), and fires the
// processing topic.
func TestOnLinearEvent_MirrorsHumanComment(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["d2"] = linearCommentPayload("create", "c2", "LGTM", "issue1", "", "Ada Lovelace", "ada@x.io", false)
	st.issueToChannel["org1|issue1"] = "C1"
	st.channelToIssue["org1|C1"] = "issue1"

	sl := &fakeSlack{
		nextTS:       "1700000000.000009",
		usersByEmail: map[string]slackapi.User{"ada@x.io": {ID: "U_ADA", Name: "Ada Lovelace", IconURL: "https://x.io/ada.png"}},
	}
	pub := &spyPub{}
	e := newEngine(st, sl, &fakeIntg{}, pub)

	e.OnLinearEvent(context.Background(), linearRef("d2", "org1"))

	if len(sl.posted) != 1 {
		t.Fatalf("want 1 Slack post, got %d", len(sl.posted))
	}
	got := sl.posted[0]
	if got.ChannelID != "C1" || got.Text != "LGTM" {
		t.Errorf("post routing wrong: %+v", got)
	}
	if got.Username != "Ada Lovelace" || got.IconURL != "https://x.io/ada.png" {
		t.Errorf("attribution not applied: username=%q icon=%q", got.Username, got.IconURL)
	}
	if !pub.has(TopicSlackMessage) {
		t.Error("expected sync.slack.message.posted")
	}
	// The link must be recorded so the echo can be dropped and threads resolved.
	if len(st.recorded) != 1 || st.recorded[0].LinearCommentID != "c2" || st.recorded[0].SlackTS != "1700000000.000009" {
		t.Errorf("mirror link not recorded correctly: %+v", st.recorded)
	}
}

// A Linear reply (parentId set) must be posted into the Slack thread of the
// parent's mirror (thread-parent resolution via the routing map).
func TestOnLinearEvent_ReplyGoesToThread(t *testing.T) {
	st := newFakeStore()
	st.issueToChannel["org1|issue1"] = "C1"
	st.channelToIssue["org1|C1"] = "issue1"
	// Parent comment c_root already mirrored to Slack ts=ROOT.
	st.linksByLinear["org1|c_root"] = store.MirroredMessage{
		OrgID: "org1", LinearCommentID: "c_root", SlackChannelID: "C1", SlackTS: "ROOT", RootSlackTS: "ROOT",
	}
	st.linearPayloads["d3"] = linearCommentPayload("create", "c_reply", "a reply", "issue1", "c_root", "Ada", "ada@x.io", false)

	sl := &fakeSlack{nextTS: "REPLYTS"}
	pub := &spyPub{}
	e := newEngine(st, sl, &fakeIntg{}, pub)

	e.OnLinearEvent(context.Background(), linearRef("d3", "org1"))

	if len(sl.posted) != 1 {
		t.Fatalf("want 1 post, got %d", len(sl.posted))
	}
	if sl.posted[0].ThreadTS != "ROOT" {
		t.Errorf("reply should thread under ROOT, got thread_ts=%q", sl.posted[0].ThreadTS)
	}
	// Recorded link should carry the root anchor for further replies.
	if st.recorded[0].RootSlackTS != "ROOT" {
		t.Errorf("recorded root ts = %q, want ROOT", st.recorded[0].RootSlackTS)
	}
}

// Status-trigger creates the channel when an issue reaches the configured
// status, names it from the template, auto-adds bots, and fires the topics.
func TestOnLinearEvent_StatusTriggerCreatesChannel(t *testing.T) {
	st := newFakeStore()
	env := map[string]any{
		"event_type": "linear",
		"linear": map[string]any{
			"action": "update", "type": "Issue",
			"actor": map[string]any{"name": "Ada"},
			"data":  map[string]any{"id": "issue9", "identifier": "SKO-9", "teamId": "team1", "state": map[string]any{"name": "In Progress"}},
		},
	}
	b, _ := json.Marshal(env)
	st.linearPayloads["d4"] = b

	sl := &fakeSlack{nextChannel: "C_MADE"}
	pub := &spyPub{}
	ig := &fakeIntg{settings: integrations.LinearSettings{
		CreationMode:   "status",
		TriggerStatus:  "In Progress",
		NameTemplate:   "tkt-${{ linear.data.identifier }}",
		AutoAddMembers: []string{"UBOT1", "UBOT2"},
	}}
	e := newEngine(st, sl, ig, pub)

	e.OnLinearEvent(context.Background(), linearRef("d4", "org1"))

	if sl.createdName != "tkt-sko-9" {
		t.Errorf("channel name = %q, want tkt-sko-9 (sanitized/lowercased)", sl.createdName)
	}
	if c, _ := st.ChannelForIssue(context.Background(), "org1", "issue9"); c != "C_MADE" {
		t.Errorf("issue->channel mapping not stored: %q", c)
	}
	if len(sl.invited) != 2 {
		t.Errorf("expected 2 bots invited, got %v", sl.invited)
	}
	if !pub.has(TopicChannelCreated) || !pub.has(TopicBotsAdded) {
		t.Errorf("expected channel.created + bots.added topics, got %v", pub.topics)
	}
}

// Wrong status must not create a channel.
func TestOnLinearEvent_StatusTriggerIgnoresOtherStatus(t *testing.T) {
	st := newFakeStore()
	env := map[string]any{"event_type": "linear", "linear": map[string]any{
		"action": "update", "type": "Issue", "actor": map[string]any{},
		"data": map[string]any{"id": "issue9", "identifier": "SKO-9", "teamId": "team1", "state": map[string]any{"name": "Backlog"}},
	}}
	b, _ := json.Marshal(env)
	st.linearPayloads["d5"] = b
	sl := &fakeSlack{}
	e := newEngine(st, sl, &fakeIntg{settings: integrations.LinearSettings{CreationMode: "status", TriggerStatus: "Done"}}, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("d5", "org1"))
	if sl.createdName != "" {
		t.Errorf("no channel should be created for a non-trigger status; created %q", sl.createdName)
	}
}

// Condition mode creates a channel on any issue event whose condition is true,
// regardless of the issue's status.
func TestOnLinearEvent_ConditionTriggerCreatesChannel(t *testing.T) {
	st := newFakeStore()
	env := map[string]any{"event_type": "linear", "linear": map[string]any{
		"action": "update", "type": "Issue", "actor": map[string]any{"name": "Ada"},
		"data": map[string]any{"id": "issue9", "identifier": "SKO-9", "teamId": "team1", "state": map[string]any{"name": "Done"}},
	}}
	b, _ := json.Marshal(env)
	st.linearPayloads["dc1"] = b
	sl := &fakeSlack{nextChannel: "C_COND"}
	pub := &spyPub{}
	ig := &fakeIntg{settings: integrations.LinearSettings{
		CreationMode:  "condition",
		ConditionExpr: "linear.data.state.name == 'Done'",
		NameTemplate:  "tkt-${{ linear.data.identifier }}",
	}}
	e := newEngine(st, sl, ig, pub)

	e.OnLinearEvent(context.Background(), linearRef("dc1", "org1"))

	if sl.createdName != "tkt-sko-9" {
		t.Errorf("channel name = %q, want tkt-sko-9", sl.createdName)
	}
	if c, _ := st.ChannelForIssue(context.Background(), "org1", "issue9"); c != "C_COND" {
		t.Errorf("issue->channel mapping not stored: %q", c)
	}
}

// Condition mode must not create a channel when the condition is false.
func TestOnLinearEvent_ConditionFalseDoesNotCreate(t *testing.T) {
	st := newFakeStore()
	env := map[string]any{"event_type": "linear", "linear": map[string]any{
		"action": "update", "type": "Issue", "actor": map[string]any{},
		"data": map[string]any{"id": "issue9", "identifier": "SKO-9", "teamId": "team1", "state": map[string]any{"name": "Backlog"}},
	}}
	b, _ := json.Marshal(env)
	st.linearPayloads["dc2"] = b
	sl := &fakeSlack{}
	ig := &fakeIntg{settings: integrations.LinearSettings{
		CreationMode:  "condition",
		ConditionExpr: "linear.data.state.name == 'Done'",
	}}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("dc2", "org1"))
	if sl.createdName != "" {
		t.Errorf("no channel should be created when the condition is false; created %q", sl.createdName)
	}
}

// linearIssuePayload builds an Issue event envelope for the archive tests.
func linearIssuePayload(issueID, identifier, teamID, stateName string) json.RawMessage {
	env := map[string]any{"event_type": "linear", "linear": map[string]any{
		"action": "update", "type": "Issue", "actor": map[string]any{"name": "Ada"},
		"data": map[string]any{"id": issueID, "identifier": identifier, "teamId": teamID, "state": map[string]any{"name": stateName}},
	}}
	b, _ := json.Marshal(env)
	return b
}

// Archive status-trigger: an issue with a channel reaching the archive status
// archives the channel, removes the mapping, fires channel.closed — and never
// re-creates.
func TestOnLinearEvent_ArchiveStatusTriggerArchivesChannel(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["da1"] = linearIssuePayload("issue9", "SKO-9", "team1", "Done")
	st.issueToChannel["org1|issue9"] = "C_LIVE"
	st.channelToIssue["org1|C_LIVE"] = "issue9"

	sl := &fakeSlack{}
	pub := &spyPub{}
	ig := &fakeIntg{settings: integrations.LinearSettings{
		CreationMode:  "status",
		TriggerStatus: "In Progress",
		ArchiveMode:   "status",
		ArchiveStatus: "Done",
	}}
	e := newEngine(st, sl, ig, pub)

	e.OnLinearEvent(context.Background(), linearRef("da1", "org1"))

	if sl.archivedChannel != "C_LIVE" {
		t.Errorf("archived channel = %q, want C_LIVE", sl.archivedChannel)
	}
	if _, err := st.ChannelForIssue(context.Background(), "org1", "issue9"); err == nil {
		t.Error("issue->channel mapping should be removed after archiving")
	}
	if !pub.has(TopicChannelClosed) {
		t.Errorf("expected channel.closed topic, got %v", pub.topics)
	}
	if sl.createdName != "" {
		t.Errorf("an existing channel must never be re-created; created %q", sl.createdName)
	}
}

// A status other than the archive status must not archive.
func TestOnLinearEvent_ArchiveStatusIgnoresOtherStatus(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["da2"] = linearIssuePayload("issue9", "SKO-9", "team1", "Backlog")
	st.issueToChannel["org1|issue9"] = "C_LIVE"
	st.channelToIssue["org1|C_LIVE"] = "issue9"

	sl := &fakeSlack{}
	ig := &fakeIntg{settings: integrations.LinearSettings{
		CreationMode: "manual", ArchiveMode: "status", ArchiveStatus: "Done",
	}}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("da2", "org1"))
	if sl.archivedChannel != "" {
		t.Errorf("non-archive status must not archive; archived %q", sl.archivedChannel)
	}
}

// Archive condition-trigger: archives when the expression is true.
func TestOnLinearEvent_ArchiveConditionTriggerArchivesChannel(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["da3"] = linearIssuePayload("issue9", "SKO-9", "team1", "Done")
	st.issueToChannel["org1|issue9"] = "C_LIVE"
	st.channelToIssue["org1|C_LIVE"] = "issue9"

	sl := &fakeSlack{}
	pub := &spyPub{}
	ig := &fakeIntg{settings: integrations.LinearSettings{
		CreationMode:         "manual",
		ArchiveMode:          "condition",
		ArchiveConditionExpr: "linear.data.state.name == 'Done'",
	}}
	e := newEngine(st, sl, ig, pub)

	e.OnLinearEvent(context.Background(), linearRef("da3", "org1"))

	if sl.archivedChannel != "C_LIVE" {
		t.Errorf("archived channel = %q, want C_LIVE", sl.archivedChannel)
	}
	if !pub.has(TopicChannelClosed) {
		t.Errorf("expected channel.closed topic, got %v", pub.topics)
	}
}

// A false archive condition must not archive.
func TestOnLinearEvent_ArchiveConditionFalseDoesNotArchive(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["da4"] = linearIssuePayload("issue9", "SKO-9", "team1", "Backlog")
	st.issueToChannel["org1|issue9"] = "C_LIVE"
	st.channelToIssue["org1|C_LIVE"] = "issue9"

	sl := &fakeSlack{}
	ig := &fakeIntg{settings: integrations.LinearSettings{
		CreationMode:         "manual",
		ArchiveMode:          "condition",
		ArchiveConditionExpr: "linear.data.state.name == 'Done'",
	}}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("da4", "org1"))
	if sl.archivedChannel != "" {
		t.Errorf("false condition must not archive; archived %q", sl.archivedChannel)
	}
}

// Manual archive mode (and the empty default) never auto-archives.
func TestOnLinearEvent_ManualArchiveModeNeverAutoArchives(t *testing.T) {
	for _, mode := range []string{"manual", ""} {
		st := newFakeStore()
		st.linearPayloads["da5"] = linearIssuePayload("issue9", "SKO-9", "team1", "Done")
		st.issueToChannel["org1|issue9"] = "C_LIVE"
		st.channelToIssue["org1|C_LIVE"] = "issue9"

		sl := &fakeSlack{}
		ig := &fakeIntg{settings: integrations.LinearSettings{
			CreationMode: "manual", ArchiveMode: mode, ArchiveStatus: "Done",
		}}
		e := newEngine(st, sl, ig, &spyPub{})

		e.OnLinearEvent(context.Background(), linearRef("da5", "org1"))
		if sl.archivedChannel != "" {
			t.Errorf("archiveMode %q must not auto-archive; archived %q", mode, sl.archivedChannel)
		}
	}
}

// An archive trigger for an issue with no channel does nothing (and must not
// create one either when creation mode wouldn't).
func TestOnLinearEvent_ArchiveTriggerWithoutChannelDoesNothing(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["da6"] = linearIssuePayload("issue9", "SKO-9", "team1", "Done")

	sl := &fakeSlack{}
	ig := &fakeIntg{settings: integrations.LinearSettings{
		CreationMode: "manual", ArchiveMode: "status", ArchiveStatus: "Done",
	}}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("da6", "org1"))
	if sl.archivedChannel != "" || sl.createdName != "" {
		t.Errorf("no channel exists: nothing should be archived (%q) or created (%q)",
			sl.archivedChannel, sl.createdName)
	}
}

// An issue whose team isn't mapped to any config must be ignored, even when its
// status would otherwise trigger creation.
func TestOnLinearEvent_UnmappedTeamIsIgnored(t *testing.T) {
	st := newFakeStore()
	env := map[string]any{"event_type": "linear", "linear": map[string]any{
		"action": "update", "type": "Issue", "actor": map[string]any{},
		"data": map[string]any{"id": "issue9", "identifier": "SKO-9", "teamId": "teamB", "state": map[string]any{"name": "In Progress"}},
	}}
	b, _ := json.Marshal(env)
	st.linearPayloads["d6"] = b
	sl := &fakeSlack{}
	// Only teamA is mapped; the event's teamB is not → do nothing.
	ig := &fakeIntg{
		teamMapped: map[string]bool{"teamA": true},
		settings:   integrations.LinearSettings{CreationMode: "status", TriggerStatus: "In Progress"},
	}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("d6", "org1"))
	if sl.createdName != "" {
		t.Errorf("unmapped team must not create a channel; created %q", sl.createdName)
	}
}

// A WorkflowState create/update upserts the state into its team's snapshot; a
// remove deletes it. This keeps the status dropdown fresh between full syncs.
func TestOnLinearEvent_WorkflowStatePatchesSnapshot(t *testing.T) {
	workflowStatePayload := func(action, id, name string) json.RawMessage {
		env := map[string]any{"event_type": "linear", "linear": map[string]any{
			"action": action, "type": "WorkflowState", "actor": map[string]any{},
			"data": map[string]any{
				"id": id, "name": name, "type": "started", "color": "#5e6ad2", "position": 1.5,
				"team": map[string]any{"id": "teamX"},
			},
		}}
		b, _ := json.Marshal(env)
		return b
	}

	cases := []struct {
		name        string
		action      string
		wantRemoved bool
	}{
		{"create", "create", false},
		{"update", "update", false},
		{"remove", "remove", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore()
			st.linearPayloads["w1"] = workflowStatePayload(tc.action, "state1", "In Review")
			e := newEngine(st, &fakeSlack{}, &fakeIntg{}, &spyPub{})

			e.OnLinearEvent(context.Background(), linearRef("w1", "org1"))

			if len(st.statePatches) != 1 {
				t.Fatalf("want 1 state patch, got %d", len(st.statePatches))
			}
			p := st.statePatches[0]
			if p.teamID != "teamX" {
				t.Errorf("patch teamID = %q, want teamX", p.teamID)
			}
			if p.state.ID != "state1" || p.state.Name != "In Review" {
				t.Errorf("patch state = %+v, want id=state1 name=In Review", p.state)
			}
			if p.removed != tc.wantRemoved {
				t.Errorf("patch removed = %v, want %v", p.removed, tc.wantRemoved)
			}
		})
	}
}

// Slack side: a human message in a synced channel mirrors to a Linear comment
// with attribution; the created comment link is recorded.
func TestOnSlackEvent_MirrorsHumanMessage(t *testing.T) {
	st := newFakeStore()
	st.channelToIssue["org1|C1"] = "issue1"
	st.issueToChannel["org1|issue1"] = "C1"
	st.slackPayloads["e1"] = slackMessagePayload("U_HUMAN", "", "", "hello from slack", "C1", "TS1", "")

	sl := &fakeSlack{botUserID: "U_BOT", usersByID: map[string]slackapi.User{
		"U_HUMAN": {ID: "U_HUMAN", Name: "Grace Hopper", IconURL: "https://x.io/grace.png"},
	}}
	ig := &fakeIntg{nextCommentID: "cmt_1"}
	pub := &spyPub{}
	e := newEngine(st, sl, ig, pub)

	e.OnSlackEvent(context.Background(), slackRef("e1", "org1"))

	if len(ig.createdComments) != 1 {
		t.Fatalf("want 1 Linear comment, got %d", len(ig.createdComments))
	}
	c := ig.createdComments[0]
	if c.IssueID != "issue1" || c.Body != "hello from slack" {
		t.Errorf("comment routing wrong: %+v", c)
	}
	if c.CreateAsUser != "Grace Hopper" || c.DisplayIconURL != "https://x.io/grace.png" {
		t.Errorf("attribution not applied: %+v", c)
	}
	if !pub.has(TopicLinearComment) {
		t.Error("expected sync.linear.comment.posted")
	}
	if len(st.recorded) != 1 || st.recorded[0].LinearCommentID != "cmt_1" || st.recorded[0].SlackTS != "TS1" {
		t.Errorf("mirror link not recorded: %+v", st.recorded)
	}
}

// Defense 1 (Slack side): the bot's own message (bot_id set) must be dropped —
// this is the return leg of the loop.
func TestOnSlackEvent_DropsBotMessage(t *testing.T) {
	st := newFakeStore()
	st.channelToIssue["org1|C1"] = "issue1"
	st.slackPayloads["e2"] = slackMessagePayload("U_BOT", "B123", "bot_message", "echo", "C1", "TS2", "")
	ig := &fakeIntg{}
	e := newEngine(st, &fakeSlack{}, ig, &spyPub{})

	e.OnSlackEvent(context.Background(), slackRef("e2", "org1"))
	if len(ig.createdComments) != 0 {
		t.Fatalf("bot message must not create a Linear comment; got %d", len(ig.createdComments))
	}
}

// A Slack thread reply maps to a Linear reply under the parent comment.
func TestOnSlackEvent_ReplyThreadsUnderParentComment(t *testing.T) {
	st := newFakeStore()
	st.channelToIssue["org1|C1"] = "issue1"
	// The thread root Slack ts=ROOT mirrors Linear comment c_root.
	st.linksBySlack["org1|C1|ROOT"] = store.MirroredMessage{
		OrgID: "org1", LinearCommentID: "c_root", SlackChannelID: "C1", SlackTS: "ROOT", RootLinearCommentID: "c_root",
	}
	st.slackPayloads["e3"] = slackMessagePayload("U_HUMAN", "", "", "reply text", "C1", "TS3", "ROOT")

	sl := &fakeSlack{botUserID: "U_BOT"}
	ig := &fakeIntg{nextCommentID: "cmt_reply"}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnSlackEvent(context.Background(), slackRef("e3", "org1"))

	if len(ig.createdComments) != 1 {
		t.Fatalf("want 1 comment, got %d", len(ig.createdComments))
	}
	if ig.createdComments[0].ParentID != "c_root" {
		t.Errorf("reply should have ParentID=c_root, got %q", ig.createdComments[0].ParentID)
	}
}

// A message in a channel we don't sync is ignored (no issue mapping).
func TestOnSlackEvent_UnsyncedChannelIgnored(t *testing.T) {
	st := newFakeStore()
	st.slackPayloads["e4"] = slackMessagePayload("U_HUMAN", "", "", "hi", "C_UNKNOWN", "TS4", "")
	ig := &fakeIntg{}
	e := newEngine(st, &fakeSlack{botUserID: "U_BOT"}, ig, &spyPub{})

	e.OnSlackEvent(context.Background(), slackRef("e4", "org1"))
	if len(ig.createdComments) != 0 {
		t.Errorf("unsynced channel must be ignored; created %d comments", len(ig.createdComments))
	}
}

// Pub/Sub push is at-least-once: a slow-but-successful handler is redelivered
// after the ack deadline. Mirroring must be idempotent — a redelivered comment
// must post to Slack only once (finding #6, Linear->Slack).
func TestOnLinearEvent_RedeliveredCommentMirrorsOnce(t *testing.T) {
	st := newFakeStore()
	st.linearPayloads["d2"] = linearCommentPayload("create", "c2", "LGTM", "issue1", "", "Ada Lovelace", "ada@x.io", false)
	st.issueToChannel["org1|issue1"] = "C1"
	st.channelToIssue["org1|C1"] = "issue1"
	sl := &fakeSlack{nextTS: "1700000000.000009"}
	e := newEngine(st, sl, &fakeIntg{}, &spyPub{})

	e.OnLinearEvent(context.Background(), linearRef("d2", "org1"))
	e.OnLinearEvent(context.Background(), linearRef("d2", "org1")) // redelivery

	if len(sl.posted) != 1 {
		t.Fatalf("redelivery must mirror once; got %d Slack posts", len(sl.posted))
	}
	if len(st.recorded) != 1 {
		t.Fatalf("want 1 mirror link, got %d", len(st.recorded))
	}
}

// Idempotency for the Slack->Linear direction: a redelivered Slack message must
// create only one Linear comment. Each create would mint a fresh comment id, so
// the after-the-fact unique key can't dedup — the pre-write check must (finding #6).
func TestOnSlackEvent_RedeliveredMessageMirrorsOnce(t *testing.T) {
	st := newFakeStore()
	st.channelToIssue["org1|C1"] = "issue1"
	st.issueToChannel["org1|issue1"] = "C1"
	st.slackPayloads["e1"] = slackMessagePayload("U_HUMAN", "", "", "hello from slack", "C1", "TS1", "")
	sl := &fakeSlack{botUserID: "U_BOT"}
	ig := &fakeIntg{nextCommentID: "cmt_1"}
	e := newEngine(st, sl, ig, &spyPub{})

	e.OnSlackEvent(context.Background(), slackRef("e1", "org1"))
	e.OnSlackEvent(context.Background(), slackRef("e1", "org1")) // redelivery

	if len(ig.createdComments) != 1 {
		t.Fatalf("redelivery must mirror once; got %d Linear comments", len(ig.createdComments))
	}
	if len(st.recorded) != 1 {
		t.Fatalf("want 1 mirror link, got %d", len(st.recorded))
	}
}
