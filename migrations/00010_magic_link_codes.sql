-- +goose Up
ALTER TABLE magic_links ADD COLUMN code_hash TEXT;
ALTER TABLE magic_links ADD COLUMN code_attempts SMALLINT NOT NULL DEFAULT 0;

CREATE INDEX idx_magic_links_email_active
  ON magic_links (email) WHERE used_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_magic_links_email_active;
ALTER TABLE magic_links DROP COLUMN code_attempts;
ALTER TABLE magic_links DROP COLUMN code_hash;
