```sh
-- Users
CREATE TABLE users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    email VARCHAR(100) UNIQUE NOT NULL,
    phone VARCHAR(20) UNIQUE NULL,
    password VARCHAR(255) NOT NULL,
    role ENUM('admin','siswa') NOT NULL DEFAULT 'siswa'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- OTP
CREATE TABLE whatsapp_otps (
    id INT AUTO_INCREMENT PRIMARY KEY,
    phone VARCHAR(20) NOT NULL,
    otp VARCHAR(6) NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY phone_unique (phone)
);
```
