-- +goose Up
ALTER TABLE users_index
MODIFY phone VARCHAR(20) NULL;

ALTER TABLE users
MODIFY phone VARCHAR(20) NULL;

-- +goose Down
-- Phone numbers remain nullable to preserve accounts created without a phone number.
