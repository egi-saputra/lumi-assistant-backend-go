-- +goose Up
ALTER TABLE tenants
    ALTER COLUMN max_users SET DEFAULT 5;

UPDATE tenants AS tenant
LEFT JOIN accounts AS account ON account.id = tenant.owner_account_id
SET tenant.max_users = CASE
    WHEN account.subscription_plan = 'premium'
        AND account.subscription_end > CURRENT_TIMESTAMP THEN 10
    ELSE 5
END;

-- +goose Down
ALTER TABLE tenants
    ALTER COLUMN max_users SET DEFAULT 1;

UPDATE tenants AS tenant
LEFT JOIN accounts AS account ON account.id = tenant.owner_account_id
SET tenant.max_users = CASE
    WHEN account.subscription_plan = 'premium'
        AND account.subscription_end > CURRENT_TIMESTAMP THEN 10
    ELSE 1
END;
