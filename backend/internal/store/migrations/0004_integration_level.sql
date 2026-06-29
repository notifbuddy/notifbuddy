-- Add an integration "level" so a provider can be connected both at the
-- workspace level (org-wide install/bot, the original behaviour) AND at the user
-- level (a per-user OAuth token used to act as that user for two-way sync).
--
-- level='workspace' rows keep connected_user_id='' (one per org+provider, as
-- before). level='user' rows carry the connecting user's id, so many users can
-- each connect the same provider for the same org. The composite PK keeps all of
-- these distinct. Existing rows adopt the defaults (workspace / '') with no data
-- loss.
ALTER TABLE org_integrations
    ADD COLUMN IF NOT EXISTS level            text NOT NULL DEFAULT 'workspace',
    ADD COLUMN IF NOT EXISTS connected_user_id text NOT NULL DEFAULT '';

-- Repoint the primary key from (org_id, provider) to include level + user id.
-- Postgres names a table's PK "<table>_pkey" by default; drop it before adding
-- the wider key. Guarded so re-running the migration is a no-op.
ALTER TABLE org_integrations DROP CONSTRAINT IF EXISTS org_integrations_pkey;
ALTER TABLE org_integrations
    ADD PRIMARY KEY (org_id, provider, level, connected_user_id);
