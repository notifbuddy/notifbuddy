-- Remodels Linear channel-creation settings from a single row per org into
-- multiple named configs, each scoped to ONE Linear team, plus a synced snapshot
-- of every team's workflow states (to drive the status dropdown).
--
-- The previous linear_settings (PK org_id, one config per org) is dropped
-- outright — there is no meaningful mapping from the org-wide config to the new
-- per-team model, and purging the old data was explicitly approved.
--
-- gen_random_uuid() is built in on Postgres 13+; pgcrypto provides it on older
-- versions, so we ensure the extension defensively (no-op where already built in).
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Migrations run on every startup, so this must be idempotent AND self-healing:
-- it drops any linear_settings whose shape predates this file (the original
-- org-id-per-row config, or the interim version that kept teams in a join
-- table), rebuilding it once, then no-ops on subsequent boots when the shape
-- already matches. The obsolete join table is always dropped. Purging stale
-- config data was explicitly approved.
--
-- "Current shape" = has a team_id column AND no longer has the dropped `name`
-- column. If either differs, the table is from an earlier iteration and gets
-- rebuilt.
DROP TABLE IF EXISTS linear_setting_teams;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'linear_settings')
       AND (
           NOT EXISTS (
               SELECT 1 FROM information_schema.columns
               WHERE table_name = 'linear_settings' AND column_name = 'team_id'
           )
           OR EXISTS (
               SELECT 1 FROM information_schema.columns
               WHERE table_name = 'linear_settings' AND column_name = 'name'
           )
       ) THEN
        DROP TABLE linear_settings;
    END IF;
END $$;

-- A channel-creation config scoped to a single Linear team (its identity is the
-- team, so there is no separate name). setting_id is the stable,
-- externally-referenced id (URLs, API). The UNIQUE(org_id, team_id) enforces one
-- config per team: a second config claiming the same team fails at insert time.
CREATE TABLE IF NOT EXISTS linear_settings (
    setting_id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         text NOT NULL,
    -- The single Linear team this config applies to.
    team_id        text NOT NULL,
    -- 'status' = auto-create when an issue reaches trigger_status; 'manual' =
    -- only via @notifbuddy.
    creation_mode  text NOT NULL DEFAULT 'manual',
    -- Linear workflow state name that triggers creation (when mode='status').
    trigger_status text NOT NULL DEFAULT '',
    -- GitHub-Actions-expression template for the channel name.
    name_template  text NOT NULL DEFAULT '',
    -- GitHub-Actions-expression that must evaluate true for creation to proceed.
    condition_expr text NOT NULL DEFAULT '',
    -- Bots to auto-add on channel creation (e.g. ["claude","linear"]).
    auto_add_bots  jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS linear_settings_org_idx ON linear_settings (org_id);
-- One config per (org, team).
CREATE UNIQUE INDEX IF NOT EXISTS linear_settings_org_team_key
    ON linear_settings (org_id, team_id);

-- Synced snapshot of each team's workflow states, refreshed on Linear connect
-- and patched by WorkflowState webhooks. `states` is a jsonb array of
-- {id,name,type,color,position}. Powers the trigger-status dropdown.
CREATE TABLE IF NOT EXISTS linear_team_states (
    org_id    text NOT NULL,
    team_id   text NOT NULL,           -- Linear team UUID
    team_key  text NOT NULL DEFAULT '', -- e.g. "SKO"
    team_name text NOT NULL DEFAULT '',
    states    jsonb NOT NULL DEFAULT '[]'::jsonb,
    synced_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, team_id)
);
