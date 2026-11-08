-- +goose Up
-- Satu baris = satu user menyatakan memakai satu tool.
-- Katalog tools tidak ada di SQL; tool_id divalidasi service terhadap
-- snapshot markdown. Karena itu tidak ada foreign key ke tabel tools.
CREATE TABLE tool_votes (
  tool_id    TEXT NOT NULL,
  user_id    UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT tool_votes_pkey PRIMARY KEY (tool_id, user_id),
  CONSTRAINT tool_votes_user_fk
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT tool_votes_tool_id_check CHECK (
    char_length(tool_id) BETWEEN 3 AND 32
    AND tool_id ~ '^[A-Z]+-[0-9]+$'
  )
);

-- Query per user: "tool apa yang saya vote" dan cascade delete user.
CREATE INDEX idx_tool_votes_user_id ON tool_votes (user_id);

-- +goose Down
DROP TABLE IF EXISTS tool_votes;
