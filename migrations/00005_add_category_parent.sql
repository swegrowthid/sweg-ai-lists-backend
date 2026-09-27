-- +goose Up
ALTER TABLE categories
  ADD COLUMN parent_id UUID REFERENCES categories(id) ON DELETE CASCADE;

CREATE INDEX idx_categories_parent_id ON categories (parent_id);

-- +goose StatementBegin
CREATE FUNCTION categories_depth_guard() RETURNS trigger AS $$
BEGIN
  IF NEW.parent_id IS NULL THEN
    RETURN NEW;
  END IF;

  IF NEW.parent_id = NEW.id THEN
    RAISE EXCEPTION 'category cannot be its own parent';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM categories parent
    WHERE parent.id = NEW.parent_id
      AND parent.parent_id IS NOT NULL
  ) THEN
    RAISE EXCEPTION 'category depth exceeds 2 levels';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM categories child
    WHERE child.parent_id = NEW.id
  ) THEN
    RAISE EXCEPTION 'category already has children and cannot become a child';
  END IF;

  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_categories_depth_guard
BEFORE INSERT OR UPDATE OF parent_id ON categories
FOR EACH ROW EXECUTE FUNCTION categories_depth_guard();
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_categories_depth_guard ON categories;
DROP FUNCTION IF EXISTS categories_depth_guard();
DROP INDEX IF EXISTS idx_categories_parent_id;
ALTER TABLE categories DROP COLUMN IF EXISTS parent_id;
