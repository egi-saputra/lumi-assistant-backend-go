CREATE TABLE users (
    id CHAR(26) PRIMARY KEY,

    name VARCHAR(100) NOT NULL,
    email VARCHAR(100) NOT NULL,
    phone VARCHAR(20) NULL,

    password_hash VARCHAR(255) NOT NULL,

    role ENUM('admin','user') NOT NULL DEFAULT 'user',

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    UNIQUE KEY uniq_email (email),
    UNIQUE KEY uniq_phone (phone)
);