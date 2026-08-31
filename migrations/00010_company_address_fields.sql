-- +goose Up
ALTER TABLE companies ADD COLUMN street TEXT NOT NULL DEFAULT '';
ALTER TABLE companies ADD COLUMN address_line2 TEXT NOT NULL DEFAULT '';
ALTER TABLE companies ADD COLUMN postal_code TEXT NOT NULL DEFAULT '';
ALTER TABLE companies ADD COLUMN city TEXT NOT NULL DEFAULT '';
ALTER TABLE companies ADD COLUMN state TEXT NOT NULL DEFAULT '';
ALTER TABLE companies ADD COLUMN country TEXT NOT NULL DEFAULT '';
-- legacy free-text address lands in street; old column kept but unused
UPDATE companies SET street = trim(replace(replace(address, char(13), ''), char(10), ', ')) WHERE address IS NOT NULL AND address != '';

-- +goose Down
-- SQLite <3.35 does not support DROP COLUMN; leave the columns.
