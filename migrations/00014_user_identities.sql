-- +goose Up
-- External accounts linked to a local user. One row per account, one account
-- per provider per user. A swegrowth.id account is the Supabase auth user id.
CREATE TABLE user_identities (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider TEXT NOT NULL CHECK (provider <> ''),
  provider_user_id TEXT NOT NULL CHECK (provider_user_id <> ''),
  linked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT user_identities_pkey PRIMARY KEY (provider, provider_user_id),
  CONSTRAINT user_identities_user_provider_key UNIQUE (user_id, provider)
);

-- linked_at uses users.updated_at: the linking code moved that column, so it
-- is the best available upper bound for the original link time.
INSERT INTO user_identities (user_id, provider, provider_user_id, linked_at)
SELECT id, 'swegrowth', supabase_id::text, updated_at
FROM users
WHERE supabase_id IS NOT NULL;

ALTER TABLE users DROP COLUMN supabase_id;

-- +goose Down
-- Safe today because swegrowth is the only provider. The UUID cast fails
-- loudly on a non-UUID value. Rows for other providers are dropped.
ALTER TABLE users ADD COLUMN supabase_id UUID UNIQUE;

UPDATE users u
SET supabase_id = i.provider_user_id::uuid
FROM user_identities i
WHERE i.user_id = u.id AND i.provider = 'swegrowth';

DROP TABLE user_identities;
