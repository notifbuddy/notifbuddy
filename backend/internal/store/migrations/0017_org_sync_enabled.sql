ALTER TABLE org_profile
    ADD COLUMN IF NOT EXISTS sync_enabled boolean NOT NULL DEFAULT true;
