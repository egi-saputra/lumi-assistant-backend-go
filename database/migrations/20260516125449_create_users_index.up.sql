CREATE TABLE users_index (
    user_id CHAR(26) PRIMARY KEY,

    tenant_id CHAR(26) NOT NULL,

    email VARCHAR(100) NOT NULL,
    phone VARCHAR(20) NULL,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uniq_tenant_email (tenant_id, email),
    INDEX idx_tenant (tenant_id)
);