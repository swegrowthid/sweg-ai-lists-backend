-- +goose Up
-- Tabel news: satu baris per post blog sumber yang lolos filter judul.
-- Sumber: https://www.zainfathoni.com/blog dengan filter "AI Tools Digest".
CREATE TABLE news (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title        TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  url          TEXT NOT NULL UNIQUE CHECK (url ~ '^https?://' AND char_length(url) <= 500),
  summary      TEXT NOT NULL DEFAULT '' CHECK (char_length(summary) <= 1000),
  published_at TIMESTAMPTZ NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE TRIGGER trg_news_updated_at
BEFORE UPDATE ON news
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- Index daftar news: terbit terbaru dulu, id sebagai tie-break.
CREATE INDEX idx_news_published_at ON news (published_at DESC, id);

-- +goose Down
DROP TRIGGER IF EXISTS trg_news_updated_at ON news;
DROP TABLE IF EXISTS news;
