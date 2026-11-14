-- +goose Up
-- The swegrowth.id account (Supabase auth user id) a local user signs in with.
-- Null for accounts that only use a password or magic link.
ALTER TABLE users ADD COLUMN supabase_id UUID UNIQUE;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS supabase_id;
