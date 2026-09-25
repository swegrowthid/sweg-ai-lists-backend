-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE posts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug       TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  title      TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE TRIGGER trg_posts_updated_at
BEFORE UPDATE ON posts
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- Index trigram untuk pencarian judul dengan ILIKE.
CREATE INDEX idx_posts_title_trgm ON posts USING gin (title gin_trgm_ops);

CREATE TABLE post_categories (
  post_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
  PRIMARY KEY (post_id, category_id)
);

-- Index untuk lookup balik: filter post berdasarkan slug category.
CREATE INDEX idx_post_categories_category ON post_categories (category_id);

CREATE TABLE post_items (
  id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  kind    TEXT NOT NULL CHECK (kind IN ('markdown', 'text', 'link', 'file')),
  position INTEGER NOT NULL CHECK (position >= 0),
  -- body_text: isi markdown, text, atau file .md inline
  body_text TEXT,
  -- url: link tujuan, hanya untuk kind 'link'
  url TEXT,
  -- filename: nama file, hanya untuk kind 'file'
  filename TEXT,
  -- mime: tipe MIME file, opsional untuk kind 'file'
  mime TEXT,
  -- cdn_url: cadangan nullable untuk CDN, belum dipakai di v1
  cdn_url TEXT,
  -- cdn_provider: cadangan nullable untuk CDN, belum dipakai di v1
  cdn_provider TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (post_id, position),
  CONSTRAINT post_items_kind_payload CHECK (
    (kind = 'link' AND url IS NOT NULL AND body_text IS NULL AND filename IS NULL AND mime IS NULL)
    OR (kind IN ('markdown', 'text') AND body_text IS NOT NULL AND url IS NULL AND filename IS NULL AND mime IS NULL)
    OR (kind = 'file' AND body_text IS NOT NULL AND filename IS NOT NULL AND url IS NULL)
  )
);

-- +goose Down
DROP TABLE IF EXISTS post_items;
DROP TABLE IF EXISTS post_categories;
DROP TABLE IF EXISTS posts;
