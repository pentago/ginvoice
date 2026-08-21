-- +goose Up
ALTER TABLE companies ADD COLUMN logo_data TEXT;

-- +goose Down
ALTER TABLE companies DROP COLUMN logo_data;
