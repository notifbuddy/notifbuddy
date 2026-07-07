-- Billing state per org. Orgs/users/memberships live in WorkOS, so org_id is
-- the WorkOS org id (text, no FK). We store facts only (plan, raw Stripe
-- subscription status, trial deadline); whether an org is "locked" is derived
-- at read time — there is no cron to flip flags when a trial lapses.
CREATE TABLE IF NOT EXISTS org_billing (
    org_id                 text PRIMARY KEY,
    -- trial | pro | oss_free | enterprise
    plan                   text NOT NULL DEFAULT 'trial',
    stripe_customer_id     text UNIQUE,
    stripe_subscription_id text,
    -- Raw Stripe subscription status (active, past_due, canceled, ...).
    stripe_status          text,
    -- Last seat quantity pushed to the Stripe subscription.
    seats                  int,
    trial_ends_at          timestamptz NOT NULL,
    -- Open-source free-tier application (approval is manual, via SQL).
    sponsor_url            text,
    sponsor_note           text,
    -- NULL | pending | approved | rejected
    oss_application_status text,
    oss_applied_at         timestamptz,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now()
);

-- Durable Stripe webhook deliveries, idempotent on Stripe's event id (Stripe
-- retries deliveries; a duplicate insert is a no-op and short-circuits
-- processing). Same shape as the other *_webhook_events tables.
CREATE TABLE IF NOT EXISTS stripe_webhook_events (
    id          bigserial PRIMARY KEY,
    event_id    text NOT NULL UNIQUE,
    event_type  text NOT NULL,
    org_id      text,
    payload     jsonb NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS stripe_webhook_events_org_idx
    ON stripe_webhook_events (org_id, received_at DESC);

-- Durable WorkOS webhook deliveries (organization_membership.* events drive
-- seat sync), idempotent on the WorkOS event id.
CREATE TABLE IF NOT EXISTS workos_webhook_events (
    id          bigserial PRIMARY KEY,
    event_id    text NOT NULL UNIQUE,
    event_type  text NOT NULL,
    org_id      text,
    payload     jsonb NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS workos_webhook_events_org_idx
    ON workos_webhook_events (org_id, received_at DESC);
