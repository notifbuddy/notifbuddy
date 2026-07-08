-- Archive triggers for Linear channel configs, mirroring the creation
-- trigger columns. 'manual' means no auto-archive (channels are only closed
-- via @notifbuddy), so existing configs keep today's behavior.
ALTER TABLE linear_settings
    ADD COLUMN IF NOT EXISTS archive_mode           text NOT NULL DEFAULT 'manual',
    ADD COLUMN IF NOT EXISTS archive_status         text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS archive_condition_expr text NOT NULL DEFAULT '';
