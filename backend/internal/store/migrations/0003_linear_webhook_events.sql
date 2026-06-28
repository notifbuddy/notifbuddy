-- linear_webhook_events stores every Linear webhook delivery we receive, after
-- HMAC verification. This is the durable source of truth for the Linear webhooks
-- view; publishing the integrations.linear.webhook_event notification is a
-- separate operation that does not write here. Mirrors github_webhook_events.
CREATE TABLE IF NOT EXISTS linear_webhook_events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    delivery_id     text NOT NULL,                 -- derived id (webhookId:webhookTimestamp), unique per delivery
    event_type      text NOT NULL,                 -- payload.type (e.g. Issue, Comment, Project)
    workspace_id    text,                           -- payload.organizationId, when present
    org_id          text,                           -- resolved from workspace_id, when known
    action          text,                           -- payload.action (create, update, remove)
    payload         jsonb NOT NULL,
    received_at     timestamptz NOT NULL DEFAULT now()
);

-- Idempotency: Linear may redeliver; a delivery id maps to one row.
CREATE UNIQUE INDEX IF NOT EXISTS linear_webhook_events_delivery_id_key
    ON linear_webhook_events (delivery_id);

-- The view lists an org's recent events newest-first.
CREATE INDEX IF NOT EXISTS linear_webhook_events_org_received_idx
    ON linear_webhook_events (org_id, received_at DESC);
