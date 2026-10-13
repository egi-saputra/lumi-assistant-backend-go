-- +goose Up
CREATE TABLE IF NOT EXISTS accounts (
    id CHAR(26) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(100) NOT NULL,
    phone VARCHAR(20) NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uniq_accounts_email (email)
);

ALTER TABLE users_index
DROP PRIMARY KEY,
ADD PRIMARY KEY (user_id, tenant_id);

-- +goose Down
ALTER TABLE users_index
DROP PRIMARY KEY,
ADD PRIMARY KEY (user_id);

DROP TABLE accounts;
