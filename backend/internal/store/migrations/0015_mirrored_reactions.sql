-- Links a mirrored reaction across tools. event_source/event_source_id follow
-- mirrored_assets: envelope vocabulary ("linear" today) and that system's
-- native reaction id (Linear reaction UUID). parent_* is the containing
-- message/comment; counterpart_* is the other side's parent id + emoji form.
-- Used so Slack → Linear remove can call reactionDelete by UUID without a
-- GraphQL scan. Loop prevention remains Defense 1 at the engine layer.
CREATE TABLE IF NOT EXISTS mirrored_reactions (
    org_id                 text NOT NULL,
    event_source           text NOT NULL,  -- owns event_source_id: "linear", ...
    event_source_id        text NOT NULL,  -- native reaction id (Linear UUID)
    parent_source          text NOT NULL,  -- containing object: "linear", ...
    parent_source_id       text NOT NULL,  -- e.g. Linear comment id
    emoji                  text NOT NULL,  -- form used by event_source (Unicode on Linear)
    counterpart_source     text NOT NULL,  -- "slack" today
    counterpart_parent_id  text NOT NULL,  -- e.g. Slack "channel_id:ts"
    counterpart_emoji      text NOT NULL,  -- form used by counterpart (Slack shortcode)
    created_at             timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, event_source, event_source_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS mirrored_reactions_counterpart_key
    ON mirrored_reactions (org_id, counterpart_source, counterpart_parent_id, counterpart_emoji);
