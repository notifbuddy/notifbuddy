-- envelope_published tracks whether the writer consumer has published the
-- processed-topic envelope for a stored webhook delivery. It closes the gap
-- where the insert commits but the envelope publish fails: the redelivery
-- sees inserted=false + envelope_published=false and retries the publish
-- instead of skipping it.
--
-- The column add + backfill run only once (guarded by column existence), so
-- the every-startup migration pass can't clobber rows that are legitimately
-- false. Pre-existing rows are backfilled to true: they were handled under
-- the old flow.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'github_webhook_events' AND column_name = 'envelope_published'
    ) THEN
        ALTER TABLE github_webhook_events ADD COLUMN envelope_published boolean NOT NULL DEFAULT false;
        UPDATE github_webhook_events SET envelope_published = true;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'linear_webhook_events' AND column_name = 'envelope_published'
    ) THEN
        ALTER TABLE linear_webhook_events ADD COLUMN envelope_published boolean NOT NULL DEFAULT false;
        UPDATE linear_webhook_events SET envelope_published = true;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'slack_webhook_events' AND column_name = 'envelope_published'
    ) THEN
        ALTER TABLE slack_webhook_events ADD COLUMN envelope_published boolean NOT NULL DEFAULT false;
        UPDATE slack_webhook_events SET envelope_published = true;
    END IF;
END $$;
