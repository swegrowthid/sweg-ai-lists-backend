-- +goose Up
INSERT INTO categories (slug, name) VALUES
  ('coding', 'Coding'),
  ('creative', 'Creative'),
  ('presentation', 'Presentation')
ON CONFLICT (slug) DO NOTHING;

-- +goose Down
DELETE FROM categories WHERE slug IN ('coding', 'creative', 'presentation');
