-- +goose Up
ALTER TABLE accounts
    ADD COLUMN free_invoice_bonus_credits INT NOT NULL DEFAULT 0;

CREATE TABLE invoice_quota_uses (
    id CHAR(26) PRIMARY KEY,
    account_id CHAR(26) NOT NULL,
    tenant_id CHAR(26) NOT NULL,
    invoice_id CHAR(26) NULL,
    status ENUM('reserved', 'consumed', 'released') NOT NULL DEFAULT 'reserved',
    reserved_until DATETIME NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uniq_invoice_quota_invoice (invoice_id),
    INDEX idx_invoice_quota_account_status (account_id, status, reserved_until)
);

CREATE TABLE invoice_quota_reward_attempts (
    id CHAR(26) PRIMARY KEY,
    account_id CHAR(26) NOT NULL,
    reward_type ENUM('ad', 'fallback') NOT NULL,
    status ENUM('started', 'completed', 'cancelled') NOT NULL DEFAULT 'started',
    transaction_id VARCHAR(100) NULL,
    started_at DATETIME NOT NULL,
    last_heartbeat_at DATETIME NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uniq_invoice_reward_transaction (transaction_id),
    INDEX idx_invoice_reward_account_status (account_id, status)
);

-- +goose Down
DROP TABLE invoice_quota_reward_attempts;
DROP TABLE invoice_quota_uses;
ALTER TABLE accounts DROP COLUMN free_invoice_bonus_credits;
