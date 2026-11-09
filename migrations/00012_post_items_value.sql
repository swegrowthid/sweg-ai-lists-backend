-- +goose Up
-- Sederhanakan post_items: satu kolom value menggantikan kind dan payload
-- per kind. Satu post tetap menyimpan satu item di position 0.
ALTER TABLE post_items ADD COLUMN value TEXT;

-- Backfill: kind 'link' mengambil url, kind lain mengambil body_text.
UPDATE post_items
SET value = CASE WHEN kind = 'link' THEN url ELSE body_text END;

ALTER TABLE post_items ALTER COLUMN value SET NOT NULL;
ALTER TABLE post_items DROP CONSTRAINT post_items_kind_payload;
ALTER TABLE post_items DROP COLUMN kind;
ALTER TABLE post_items DROP COLUMN body_text;
ALTER TABLE post_items DROP COLUMN url;
ALTER TABLE post_items DROP COLUMN filename;
ALTER TABLE post_items DROP COLUMN mime;
ALTER TABLE post_items DROP COLUMN cdn_url;
ALTER TABLE post_items DROP COLUMN cdn_provider;

-- value kosong atau hanya spasi tidak valid.
ALTER TABLE post_items ADD CONSTRAINT post_items_value_check CHECK (btrim(value) <> '');

-- +goose Down
-- Pulihkan bentuk lama: semua item lama menjadi kind 'markdown'.
ALTER TABLE post_items DROP CONSTRAINT post_items_value_check;
ALTER TABLE post_items ADD COLUMN kind TEXT NOT NULL DEFAULT 'markdown';
ALTER TABLE post_items ALTER COLUMN kind DROP DEFAULT;
ALTER TABLE post_items ADD COLUMN body_text TEXT;
ALTER TABLE post_items ADD COLUMN url TEXT;
ALTER TABLE post_items ADD COLUMN filename TEXT;
ALTER TABLE post_items ADD COLUMN mime TEXT;
ALTER TABLE post_items ADD COLUMN cdn_url TEXT;
ALTER TABLE post_items ADD COLUMN cdn_provider TEXT;

UPDATE post_items SET body_text = value;

ALTER TABLE post_items DROP COLUMN value;
ALTER TABLE post_items ADD CONSTRAINT post_items_kind_payload CHECK (
  (kind = 'link' AND url IS NOT NULL AND body_text IS NULL AND filename IS NULL AND mime IS NULL)
  OR (kind IN ('markdown', 'text') AND body_text IS NOT NULL AND url IS NULL AND filename IS NULL AND mime IS NULL)
  OR (kind = 'file' AND body_text IS NOT NULL AND filename IS NOT NULL AND url IS NULL)
);
