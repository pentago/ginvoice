-- +goose Up
ALTER TABLE companies ADD COLUMN cc_invoice_emails INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE companies DROP COLUMN cc_invoice_emails;
