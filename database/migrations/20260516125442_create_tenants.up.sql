CREATE TABLE tenants (
    id CHAR(26) PRIMARY KEY,  -- ULID

    code VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    db_name VARCHAR(100) NOT NULL,

    plan ENUM('free', 'month', 'quarter', 'year') NOT NULL DEFAULT 'free',

    max_users INT NOT NULL DEFAULT 1,

    status ENUM('provisioning', 'active', 'suspended', 'expired', 'trial'),

    subscription_start DATETIME NULL,
    subscription_end DATETIME NULL,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    INDEX idx_status (status),
    INDEX idx_plan (plan),
    INDEX idx_subscription_end (subscription_end)
);