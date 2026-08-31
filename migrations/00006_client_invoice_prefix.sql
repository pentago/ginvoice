-- +goose Up
ALTER TABLE clients ADD COLUMN invoice_number_prefix TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite <3.35 does not support DROP COLUMN; leave the column.
