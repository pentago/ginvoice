-- +goose Up
ALTER TABLE invoices ADD COLUMN discount_bps    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE invoices ADD COLUMN discount_amount INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite <3.35 does not support DROP COLUMN; leave the columns.
