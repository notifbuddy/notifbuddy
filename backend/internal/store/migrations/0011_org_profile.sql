-- Editable organization profile bits that don't live in WorkOS. The name
-- stays in WorkOS; this table holds the avatar: a random seed for the
-- client-rendered generated avatar, plus the optional uploaded image. Rows
-- are created lazily on the org's first profile read.
CREATE TABLE IF NOT EXISTS org_profile (
    org_id              text PRIMARY KEY,
    -- Seed for the generated (marble) avatar; re-rolled on "regenerate".
    avatar_seed         text NOT NULL,
    -- Uploaded avatar image; NULL means "render the generated avatar".
    avatar_image        bytea,
    -- image/png, image/jpeg, or image/webp; '' while avatar_image is NULL.
    avatar_content_type text NOT NULL DEFAULT '',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
