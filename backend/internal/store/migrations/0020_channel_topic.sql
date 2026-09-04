-- Channel topic backlink (NOT-11): configs get an optional topic template
-- (empty = the built-in default), and issue_channels remembers the last topic
-- we set so live updates only call Slack when the rendered topic changed.
ALTER TABLE linear_settings
    ADD COLUMN IF NOT EXISTS topic_template text NOT NULL DEFAULT '';
ALTER TABLE issue_channels
    ADD COLUMN IF NOT EXISTS topic text NOT NULL DEFAULT '';
