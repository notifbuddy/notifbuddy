-- Pre-launch waitlist signups from the public landing page. Email is the
-- identity: re-submitting the same address is a no-op (idempotent form).
CREATE TABLE IF NOT EXISTS waitlist (
	email      text        PRIMARY KEY,
	created_at timestamptz NOT NULL DEFAULT now()
);
