-- linear_settings holds an organization's Linear → Slack channel-creation rules.
-- One row per org (only meaningful when Linear is connected at the workspace
-- level). All fields are optional; absence means "manual creation, no rule".
CREATE TABLE IF NOT EXISTS linear_settings (
    org_id          text PRIMARY KEY,
    -- 'status' = auto-create when an issue reaches trigger_status; 'manual' =
    -- only via @notifbuddy.
    creation_mode   text NOT NULL DEFAULT 'manual',
    -- The Linear workflow state name that triggers creation (when mode='status').
    trigger_status  text NOT NULL DEFAULT '',
    -- GitHub-Actions-expression template for the channel name.
    name_template   text NOT NULL DEFAULT '',
    -- GitHub-Actions-expression that must evaluate true for creation to proceed.
    condition_expr  text NOT NULL DEFAULT '',
    -- Bots to auto-add on channel creation (e.g. ["claude","linear"]).
    auto_add_bots   jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
