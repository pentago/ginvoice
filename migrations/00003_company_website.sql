-- +goose Up
ALTER TABLE companies ADD COLUMN website TEXT;

-- +goose Down
ALTER TABLE companies DROP COLUMN website;
