-- slack_webhook_events stores every inbound Slack Events API delivery we
-- receive, after signature verification. This is the durable source of truth
-- for the Slack side of the bidirectional sync; publishing the
-- integrations.slack.webhook_event notification is a separate operation that
-- does not write here. Mirrors linear_webhook_events / github_webhook_events.
CREATE TABLE IF NOT EXISTS slack_webhook_events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id        text NOT NULL,                 -- Slack event_id, unique per delivery
    event_type      text NOT NULL,                 -- inner event type (e.g. message)
    team_id         text,                           -- Slack team/workspace id, when present
    org_id          text,                           -- resolved from team_id, when known
    channel_id      text,                           -- the channel the event occurred in
    payload         jsonb NOT NULL,
    received_at     timestamptz NOT NULL DEFAULT now()
);

-- Idempotency: Slack retries deliveries; an event id maps to one row.
CREATE UNIQUE INDEX IF NOT EXISTS slack_webhook_events_event_id_key
    ON slack_webhook_events (event_id);

-- The view lists an org's recent events newest-first.
CREATE INDEX IF NOT EXISTS slack_webhook_events_org_received_idx
    ON slack_webhook_events (org_id, received_at DESC);
