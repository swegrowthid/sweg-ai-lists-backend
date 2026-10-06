-- +goose Up
-- Tabel wanted_posts: daftar wanted publik untuk post, tanpa login.
-- Satu baris per post yang ditandai wanted. POST duplikat bersifat idempotent.
CREATE TABLE wanted_posts (
  post_id    UUID NOT NULL PRIMARY KEY REFERENCES posts(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index daftar wanted: paling baru ditandai dulu, post_id sebagai tie-break.
CREATE INDEX idx_wanted_posts_created_at ON wanted_posts (created_at DESC, post_id);

-- +goose Down
DROP TABLE IF EXISTS wanted_posts;
