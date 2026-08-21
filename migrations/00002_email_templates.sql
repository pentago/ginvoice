-- +goose Up
ALTER TABLE companies ADD COLUMN owner_first_name TEXT;
ALTER TABLE companies ADD COLUMN owner_last_name TEXT;
ALTER TABLE companies ADD COLUMN default_email_subject TEXT;
ALTER TABLE companies ADD COLUMN default_email_body TEXT;

ALTER TABLE clients ADD COLUMN email_subject TEXT;
ALTER TABLE clients ADD COLUMN email_body TEXT;

-- +goose Down
ALTER TABLE clients DROP COLUMN email_body;
ALTER TABLE clients DROP COLUMN email_subject;
ALTER TABLE companies DROP COLUMN default_email_body;
ALTER TABLE companies DROP COLUMN default_email_subject;
ALTER TABLE companies DROP COLUMN owner_last_name;
ALTER TABLE companies DROP COLUMN owner_first_name;
