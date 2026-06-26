-- github_webhook_events stores every GitHub webhook delivery we receive, after
-- HMAC verification. This is the durable source of truth for the webhooks view;
-- publishing the integrations.github.webhook_event notification is a separate
-- operation that does not write here.
CREATE TABLE IF NOT EXISTS github_webhook_events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    delivery_id     text NOT NULL,                 -- X-GitHub-Delivery (unique per delivery)
    event_type      text NOT NULL,                 -- X-GitHub-Event (e.g. push, pull_request)
    installation_id text,                           -- from the payload, when present
    org_id          text,                           -- resolved from installation_id, when known
    action          text,                           -- payload.action, when present
    payload         jsonb NOT NULL,
    received_at     timestamptz NOT NULL DEFAULT now()
);

-- Idempotency: GitHub may redeliver; a delivery id maps to one row.
CREATE UNIQUE INDEX IF NOT EXISTS github_webhook_events_delivery_id_key
    ON github_webhook_events (delivery_id);

-- The view lists an org's recent events newest-first.
CREATE INDEX IF NOT EXISTS github_webhook_events_org_received_idx
    ON github_webhook_events (org_id, received_at DESC);
