-- Per-actor reaction mirroring: multiple users can react with the same emoji
-- on one message. counterpart_actor_id is the Slack U… id when we acted (or
-- mirrored) as a linked user; empty when the bot/app fell back. acting_user_id
-- is the NotifBuddy user id used for Linear token selection on delete.
ALTER TABLE mirrored_reactions
    ADD COLUMN IF NOT EXISTS counterpart_actor_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS acting_user_id text NOT NULL DEFAULT '';

DROP INDEX IF EXISTS mirrored_reactions_counterpart_key;

CREATE UNIQUE INDEX IF NOT EXISTS mirrored_reactions_counterpart_key
    ON mirrored_reactions (
        org_id,
        counterpart_source,
        counterpart_parent_id,
        counterpart_emoji,
        counterpart_actor_id
    );
