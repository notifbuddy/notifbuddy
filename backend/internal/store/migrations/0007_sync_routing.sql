-- Routing tables for the Slack <-> Linear bidirectional sync. These map the two
-- sides together so we can (a) find the one channel per Linear issue and (b)
-- resolve a reply's parent so threads land on the correct anchor. They are
-- ROUTING state, not a loop defense — loop prevention is handled by dropping
-- events authored by our own bot/app (Defense 1) at the engine layer.

-- issue_channels: one Slack channel per Linear issue, per org. Created when the
-- channel is opened (status trigger or @notifbuddy) and read to route inbound
-- events in both directions.
CREATE TABLE IF NOT EXISTS issue_channels (
    org_id           text NOT NULL,
    linear_issue_id  text NOT NULL,
    slack_channel_id text NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, linear_issue_id)
);

-- A channel belongs to exactly one issue within an org, so inbound Slack events
-- can resolve their issue by channel id.
CREATE UNIQUE INDEX IF NOT EXISTS issue_channels_channel_key
    ON issue_channels (org_id, slack_channel_id);

-- mirrored_messages links each mirrored comment/message to its counterpart on
-- the other side. root_* points at the thread root's counterpart so a reply on
-- one side is placed under the right parent on the other. Written the moment we
-- create the mirrored message.
CREATE TABLE IF NOT EXISTS mirrored_messages (
    id                     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id                 text NOT NULL,
    linear_comment_id      text NOT NULL,          -- the Linear comment
    slack_channel_id       text NOT NULL,
    slack_ts               text NOT NULL,          -- the Slack message ts (its id)
    -- Thread anchors: the counterpart id of the thread root. For a top-level
    -- message these equal the row's own ids; for a reply they point at the root.
    root_linear_comment_id text,
    root_slack_ts          text,
    created_at             timestamptz NOT NULL DEFAULT now()
);

-- Look up a link from either side (drop echoes / resolve thread parents).
CREATE UNIQUE INDEX IF NOT EXISTS mirrored_messages_linear_key
    ON mirrored_messages (org_id, linear_comment_id);
CREATE UNIQUE INDEX IF NOT EXISTS mirrored_messages_slack_key
    ON mirrored_messages (org_id, slack_channel_id, slack_ts);
