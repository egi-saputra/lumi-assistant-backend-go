CREATE TABLE whatsapp_otps (
    id CHAR(26) PRIMARY KEY,  -- ULID

    phone VARCHAR(20) NOT NULL,

    otp VARCHAR(6) NOT NULL,

    status ENUM('active', 'used', 'expired') DEFAULT 'active',

    attempts INT DEFAULT 0,

    expires_at DATETIME NOT NULL,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

    INDEX idx_phone (phone),
    INDEX idx_status (status),
    INDEX idx_expires (expires_at)
);