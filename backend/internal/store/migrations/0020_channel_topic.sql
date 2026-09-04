-- Channel topic backlink (NOT-11): configs get a topic template — existing
-- rows are backfilled with the default so it shows up editable in settings;
-- clearing it disables the topic. issue_channels remembers the last topic we
-- set so live updates only call Slack when the rendered topic changed.
ALTER TABLE linear_settings
    ADD COLUMN IF NOT EXISTS topic_template text NOT NULL
    DEFAULT '${{ linear.issue.identifier }}: ${{ linear.issue.title }} • ${{ linear.issue.state.name }} • ${{ linear.issue.url }}';
UPDATE linear_settings
    SET topic_template = '${{ linear.issue.identifier }}: ${{ linear.issue.title }} • ${{ linear.issue.state.name }} • ${{ linear.issue.url }}'
    WHERE topic_template = '';
ALTER TABLE issue_channels
    ADD COLUMN IF NOT EXISTS topic text NOT NULL DEFAULT '';
