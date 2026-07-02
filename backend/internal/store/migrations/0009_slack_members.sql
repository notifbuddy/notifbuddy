-- Synced snapshot of a Slack workspace's members (both bot/app users and
-- humans), refreshed on Slack connect and via the manual Sync action. Powers the
-- "auto-add bots" and "auto-add members" pickers in the Linear channel-rule
-- settings: the picker lists these members and stores their Slack member id
-- (member_id, a U… id), which is exactly what conversations.invite requires.
--
-- One row per (org, member). Slack has no member-removal webhook, so this is
-- kept fresh by full re-sync (upsert present + prune absent), not incrementally.
CREATE TABLE IF NOT EXISTS slack_members (
    org_id     text NOT NULL,
    member_id  text NOT NULL,           -- Slack U… id (what conversations.invite needs)
    name       text NOT NULL DEFAULT '', -- handle / short name
    real_name  text NOT NULL DEFAULT '', -- display / real name
    icon_url   text NOT NULL DEFAULT '', -- profile image (image_192)
    is_bot     boolean NOT NULL DEFAULT false,
    synced_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, member_id)
);
CREATE INDEX IF NOT EXISTS slack_members_org_idx ON slack_members (org_id);

-- A config auto-adds Slack members (bots and people alike) to every channel it
-- creates, stored as their Slack member ids. This single column replaces the
-- earlier bots/members split — auto_add_bots (from 0008) is dropped. Data is
-- disposable, so a drop is fine; both statements are idempotent for re-runs.
ALTER TABLE linear_settings
    ADD COLUMN IF NOT EXISTS auto_add_members jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE linear_settings
    DROP COLUMN IF EXISTS auto_add_bots;
