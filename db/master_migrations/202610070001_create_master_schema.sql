-- +goose Up
CREATE TABLE IF NOT EXISTS tenants (
    id CHAR(26) PRIMARY KEY,
    code VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    db_name VARCHAR(100) NOT NULL,
    plan ENUM('free', 'month', 'quarter', 'year') NOT NULL DEFAULT 'free',
    max_users INT NOT NULL DEFAULT 1,
    status ENUM('provisioning', 'active', 'suspended', 'expired', 'trial') DEFAULT 'provisioning',
    subscription_start DATETIME NULL,
    subscription_end DATETIME NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_status (status),
    INDEX idx_plan (plan),
    INDEX idx_subscription_end (subscription_end)
);

CREATE TABLE IF NOT EXISTS users_index (
    user_id CHAR(26) PRIMARY KEY,
    tenant_id CHAR(26) NOT NULL,
    email VARCHAR(100) NOT NULL,
    phone VARCHAR(20) NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uniq_tenant_email (tenant_id, email),
    INDEX idx_tenant (tenant_id)
);

CREATE TABLE IF NOT EXISTS users (
    id CHAR(26) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(100) NOT NULL,
    phone VARCHAR(20) NULL,
    password_hash VARCHAR(255) NOT NULL,
    role ENUM('admin', 'developer') NOT NULL DEFAULT 'developer',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uniq_email (email),
    UNIQUE KEY uniq_phone (phone)
);

-- +goose Down
DROP TABLE users_index;
DROP TABLE tenants;
DROP TABLE users;
