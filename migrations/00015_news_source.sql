-- +goose Up
-- Tabel news kini mengagregasi beberapa sumber feed. Kolom source menyimpan
-- nama tampilan sumber tiap baris. Baris lama dari listing zainfathoni.com
-- di-backfill ke nama sumbernya supaya filter dan label tetap benar.
ALTER TABLE news ADD COLUMN source TEXT NOT NULL DEFAULT ''
  CONSTRAINT news_source_len CHECK (char_length(source) <= 100);

COMMENT ON COLUMN news.source IS 'Nama tampilan sumber feed baris ini; di-backfill dari baris warisan.';

UPDATE news SET source = 'zainfathoni.com' WHERE source = '';

-- Index filter (source, q) + urutan daftar (published_at DESC, id).
CREATE INDEX idx_news_source_published_at
  ON news (source, published_at DESC, id);

-- +goose Down
DROP INDEX IF EXISTS idx_news_source_published_at;
ALTER TABLE news DROP CONSTRAINT IF EXISTS news_source_len;
ALTER TABLE news DROP COLUMN IF EXISTS source;
