-- +goose Up
ALTER TABLE companies ADD COLUMN pdf_config TEXT DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN before 3.35; leave the column.
