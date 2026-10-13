-- +goose Up
ALTER TABLE accounts
    ADD COLUMN subscription_plan ENUM('free', 'premium') NOT NULL DEFAULT 'free',
    ADD COLUMN subscription_period ENUM('monthly', 'yearly') NULL,
    ADD COLUMN subscription_start DATETIME NULL,
    ADD COLUMN subscription_end DATETIME NULL;

ALTER TABLE tenants
    ADD COLUMN owner_account_id CHAR(26) NULL,
    ADD INDEX idx_tenants_owner_account (owner_account_id);

CREATE TABLE subscription_payments (
    id CHAR(26) PRIMARY KEY,
    order_id VARCHAR(64) NOT NULL,
    account_id CHAR(26) NOT NULL,
    plan_period ENUM('monthly', 'yearly') NOT NULL,
    amount BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    snap_token VARCHAR(255) NULL,
    redirect_url TEXT NULL,
    transaction_id VARCHAR(100) NULL,
    payment_type VARCHAR(50) NULL,
    notification_payload JSON NULL,
    paid_at DATETIME NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uniq_subscription_order (order_id),
    INDEX idx_subscription_account_created (account_id, created_at),
    INDEX idx_subscription_status (status)
);

-- +goose Down
DROP TABLE subscription_payments;
ALTER TABLE tenants
    DROP INDEX idx_tenants_owner_account,
    DROP COLUMN owner_account_id;
ALTER TABLE accounts
    DROP COLUMN subscription_end,
    DROP COLUMN subscription_start,
    DROP COLUMN subscription_period,
    DROP COLUMN subscription_plan;
