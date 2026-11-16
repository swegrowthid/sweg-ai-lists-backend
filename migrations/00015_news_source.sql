-- +goose Up
-- Tabel news kini mengagregasi beberapa sumber feed. Kolom source menyimpan
-- nama tampilan sumber tiap baris; baris lama bernilai ''.
ALTER TABLE news ADD COLUMN source TEXT NOT NULL DEFAULT ''
  CONSTRAINT news_source_len CHECK (char_length(source) <= 100);

COMMENT ON COLUMN news.source IS 'Nama tampilan sumber feed baris ini; '''' untuk baris warisan.';

-- +goose Down
ALTER TABLE news DROP CONSTRAINT IF EXISTS news_source_len;
ALTER TABLE news DROP COLUMN IF EXISTS source;
