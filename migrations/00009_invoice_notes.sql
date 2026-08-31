-- +goose Up
ALTER TABLE companies ADD COLUMN invoice_notes TEXT NOT NULL DEFAULT '';
ALTER TABLE clients  ADD COLUMN invoice_notes TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite <3.35 does not support DROP COLUMN; leave the columns.
