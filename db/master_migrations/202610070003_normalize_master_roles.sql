-- +goose Up
ALTER TABLE users
MODIFY role ENUM('admin', 'user', 'developer') NOT NULL DEFAULT 'developer';

UPDATE users
SET role = 'developer'
WHERE role = 'user';

ALTER TABLE users
MODIFY role ENUM('admin', 'developer') NOT NULL DEFAULT 'developer';

-- +goose Down
-- Existing developer accounts remain developers to preserve their permissions.
