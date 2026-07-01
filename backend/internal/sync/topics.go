package sync

// Processing topics — one per action the sync engine performs. Each is
// published best-effort after the action succeeds, forming the audit/
// notification stream ("fire an event for each processing action"). They are
// dotted strings like the ingestion topics (integrations.linear.webhook_event)
// so a pubsub backend maps them to concrete destinations.
const (
	TopicChannelCreated = "sync.slack.channel.created"
	TopicChannelClosed  = "sync.slack.channel.closed"
	TopicChannelDeleted = "sync.slack.channel.deleted"
	TopicBotsAdded      = "sync.slack.bots.added"
	TopicSlackMessage   = "sync.slack.message.posted"  // a Linear comment mirrored into Slack
	TopicLinearComment  = "sync.linear.comment.posted" // a Slack message mirrored into Linear
)

// AllTopics is every processing topic, for wiring a dev logging subscriber.
var AllTopics = []string{
	TopicChannelCreated,
	TopicChannelClosed,
	TopicChannelDeleted,
	TopicBotsAdded,
	TopicSlackMessage,
	TopicLinearComment,
}

// ChannelEvent is the payload for the channel.* topics.
type ChannelEvent struct {
	OrgID         string `json:"org_id"`
	LinearIssueID string `json:"linear_issue_id"`
	SlackChannel  string `json:"slack_channel_id"`
	ChannelName   string `json:"channel_name,omitempty"`
	Trigger       string `json:"trigger"` // "status" | "notifbuddy"
}

// BotsAddedEvent is the payload for the bots.added topic.
type BotsAddedEvent struct {
	OrgID        string   `json:"org_id"`
	SlackChannel string   `json:"slack_channel_id"`
	Bots         []string `json:"bots"`
}

// MessageEvent is the payload for the message.posted / comment.posted topics: a
// comment mirrored in one direction. Direction is "linear->slack" or
// "slack->linear".
type MessageEvent struct {
	OrgID           string `json:"org_id"`
	Direction       string `json:"direction"`
	LinearIssueID   string `json:"linear_issue_id,omitempty"`
	LinearCommentID string `json:"linear_comment_id,omitempty"`
	SlackChannel    string `json:"slack_channel_id,omitempty"`
	SlackTS         string `json:"slack_ts,omitempty"`
}
