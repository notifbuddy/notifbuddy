# Billing

NotifBuddy bills per organization: **Pro at $9.99 per member per month**, a
**21-day card-less trial** for every new org, a **free-forever open-source
tier** (manual approval), and a contact-us **enterprise** plan.

## How it works

- Orgs/users/memberships live in WorkOS, so billing state keys on the WorkOS
  `org_id` (table `org_billing`, migration `0010_billing.sql`).
- The trial starts lazily: the first authenticated `/me` (or `/billing`) touch
  inserts the row with `trial_ends_at = now() + 21 days`. Lock state is derived
  at read time in `backend/internal/billing/status.go` — there is no cron.
- No Stripe objects exist until checkout. Subscribing creates a Customer +
  subscription-mode Checkout Session; the Customer Portal handles card changes
  and cancellation.
- Webhooks:
  - `POST /billing/stripe/webhook` — Stripe subscription lifecycle, processed
    synchronously with idempotent upserts (`stripe_webhook_events` dedupe).
  - `POST /auth/workos/webhook` — `organization_membership.*` events true the
    subscription's seat quantity up/down with prorations. Safety nets: the same
    reconcile runs on `invoice.upcoming` and on `GET /billing`.
- Enforcement: locked orgs get `402` on mutation endpoints (Linear settings
  CRUD, sync, template test, invitations, integration connects) and the sync
  engine drops their inbound events. `/me`, `/billing/*`, and read-only GETs
  stay open; the SPA shows a lock screen.

## One-time Stripe setup (per environment)

1. **Product + Price**: "NotifBuddy Pro", recurring $9.99/month, per-unit
   (licensed) quantity. Put the `price_...` id in `config/backend/local.yaml`
   / `config/backend/prod.yaml` under `stripe.price_id`.
2. **Restricted API key** (`rk_...`): write on Customers, Checkout Sessions,
   Subscriptions, Billing Portal; read on Invoices → `STRIPE_API_KEY`.
3. **Webhook endpoint** (prod): `<backend-url>/billing/stripe/webhook`,
   events: `checkout.session.completed`, `customer.subscription.created`,
   `customer.subscription.updated`, `customer.subscription.deleted`,
   `invoice.paid`, `invoice.payment_failed`, `invoice.upcoming` →
   `STRIPE_WEBHOOK_SECRET`. **Pin the endpoint's API version to
   `2026-06-24.dahlia`** (the version stripe-go v86 expects) — an endpoint left
   on the account default gets every delivery rejected with 401, because
   `webhook.ConstructEvent` refuses events from a different release train.
4. **Customer Portal settings**: enable payment-method updates and
   cancel-at-period-end.

And in the **WorkOS dashboard**: a webhook endpoint at
`<backend-url>/auth/workos/webhook` for
`organization_membership.created/updated/deleted` → `WORKOS_WEBHOOK_SECRET`.

## Local development

```sh
stripe listen --latest --forward-to localhost:8080/billing/stripe/webhook
# put the printed whsec_... in backend/.env as STRIPE_WEBHOOK_SECRET
```

`--latest` is required: without it the CLI renders events with the account's
default API version and stripe-go v86 rejects them (401 signature failures in
the backend log). Keep the CLI itself current (`brew upgrade
stripe/stripe-cli/stripe`) so its "latest" is on the `dahlia` train.

Test card `4242 4242 4242 4242`, any future expiry/CVC. WorkOS webhooks don't
reach localhost; seat changes true-up via `GET /billing` instead.

Missed a delivery (listener down during a checkout)? Replay it:

```sh
stripe events list --limit 10        # find the evt_... ids
stripe events resend evt_...         # forwards through the running listener
```

All webhook effects are idempotent upserts, so replays are always safe.

To exercise trial expiry locally:

```sql
UPDATE org_billing SET trial_ends_at = now() - interval '1 day' WHERE org_id = 'org_...';
```

## Approving an open-source application

Applications land as `oss_application_status = 'pending'` (visible in the org's
billing page). **Approval criteria:** the linked project must be genuinely open
source AND display a "Sponsored by NotifBuddy" tag on its README
(the billing page hands applicants a ready-made badge snippet). Check the
sponsor URL by hand before approving. Approval is manual for now (no admin UI):

```sql
-- approve
UPDATE org_billing
SET plan = 'oss_free', oss_application_status = 'approved', updated_at = now()
WHERE org_id = 'org_...';

-- reject
UPDATE org_billing
SET oss_application_status = 'rejected', updated_at = now()
WHERE org_id = 'org_...';
```

To grant an enterprise org: `UPDATE org_billing SET plan = 'enterprise' ...`.
To extend a trial (e.g. pending OSS review): bump `trial_ends_at`.
