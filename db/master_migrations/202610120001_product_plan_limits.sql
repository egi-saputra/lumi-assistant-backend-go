-- +goose Up
ALTER TABLE tenants
    ADD COLUMN free_product_bonus_slots INT NOT NULL DEFAULT 0,
    ADD COLUMN free_smart_engine_credits INT NOT NULL DEFAULT 0;

ALTER TABLE invoice_quota_reward_attempts
    MODIFY COLUMN reward_type ENUM('ad', 'fallback', 'product_slots', 'smart_engine') NOT NULL,
    ADD COLUMN tenant_id CHAR(26) NULL AFTER account_id,
    ADD INDEX idx_product_reward_tenant_status (tenant_id, reward_type, status);

-- +goose Down
DELETE FROM invoice_quota_reward_attempts
WHERE reward_type IN ('product_slots', 'smart_engine');

ALTER TABLE invoice_quota_reward_attempts
    DROP INDEX idx_product_reward_tenant_status,
    DROP COLUMN tenant_id,
    MODIFY COLUMN reward_type ENUM('ad', 'fallback') NOT NULL;

ALTER TABLE tenants
    DROP COLUMN free_smart_engine_credits,
    DROP COLUMN free_product_bonus_slots;
