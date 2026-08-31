-- +goose Up
ALTER TABLE companies RENAME COLUMN street TO address_line1;

-- +goose Down
ALTER TABLE companies RENAME COLUMN address_line1 TO street;
