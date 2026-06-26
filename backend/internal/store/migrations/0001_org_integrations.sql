-- org_integrations stores a connected third-party integration (GitHub App
-- installation, Slack workspace) for a WorkOS organization. Tokens are encrypted
-- at rest by the application (crypto.Encryptor) before being written here.
CREATE TABLE IF NOT EXISTS org_integrations (
    org_id          text        NOT NULL,
    provider        text        NOT NULL,          -- 'github' | 'slack'
    external_id     text        NOT NULL,          -- installation_id (GitHub) / team_id (Slack)
    encrypted_token bytea,                          -- sealed token; NULL for GitHub (tokens minted on demand)
    metadata        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    connected_by    text,                           -- WorkOS user_id who connected it
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, provider)
);
