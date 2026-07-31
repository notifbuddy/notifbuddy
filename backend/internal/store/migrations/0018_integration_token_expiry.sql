ALTER TABLE org_integrations
    ADD COLUMN IF NOT EXISTS token_expires_at timestamptz;
