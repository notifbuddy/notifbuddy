-- mirrored_assets records which of a mirrored Linear comment's upload URLs have
-- already been shared into Slack. Linear attaches files asynchronously: the
-- Comment create webhook carries the text only, and the attachment embed
-- arrives seconds later in a Comment update. The update handler diffs the
-- body's uploads against this table so each asset syncs exactly once, however
-- many updates (or redeliveries) follow.
-- mirrored_assets records which of a mirrored message's attachments have been
-- synced to the other side, keyed by the originating event's source system —
-- event_source matches the webhook envelope vocabulary ("linear" today;
-- "github"/"slack"/... as more tools mirror) and event_source_id is that
-- system's id for the containing object (the Linear comment id today).
--
-- inline marks images rendered inside the mirrored message's blocks (needed to
-- rebuild the full block list when a later update adds more images; the proxy
-- URL is re-derived from asset_url); false for files shared into the thread.
CREATE TABLE IF NOT EXISTS mirrored_assets (
    org_id          text NOT NULL,
    event_source    text NOT NULL,
    event_source_id text NOT NULL,
    asset_url       text NOT NULL,
    inline          boolean NOT NULL DEFAULT false,
    filename        text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, event_source, event_source_id, asset_url)
);
