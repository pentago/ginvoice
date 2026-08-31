-- +goose Up
ALTER TABLE clients ADD COLUMN currency TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite <3.35 does not support DROP COLUMN; leave the column.
