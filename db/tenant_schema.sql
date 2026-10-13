CREATE TABLE IF NOT EXISTS users (
    id CHAR(26) PRIMARY KEY,
    tenant_id CHAR(26) NOT NULL,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(100) NOT NULL,
    phone VARCHAR(20) NULL,
    password_hash VARCHAR(255) NOT NULL,
    role ENUM('owner','cashier','warehouse','courier','finance') NOT NULL DEFAULT 'cashier',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uniq_tenant_email (tenant_id, email),
    INDEX idx_tenant_role (tenant_id, role),
    INDEX idx_tenant_created (tenant_id, created_at)
);

CREATE TABLE IF NOT EXISTS profile (
    id CHAR(32) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    store VARCHAR(255) NOT NULL,
    phone VARCHAR(20) NOT NULL,
    address TEXT NULL,
    file_path TEXT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS contact (
    id CHAR(32) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    phone VARCHAR(20) NOT NULL,
    address TEXT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS social_links (
    id CHAR(32) PRIMARY KEY,
    profile_id CHAR(32) NOT NULL,
    platform VARCHAR(100) NOT NULL,
    username VARCHAR(255) NOT NULL,
    url TEXT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS payments (
    id CHAR(32) PRIMARY KEY,
    profile_id CHAR(32) NOT NULL,
    method_type VARCHAR(100) NOT NULL,
    provider_name VARCHAR(255) NOT NULL,
    account_number VARCHAR(255) NULL,
    account_holder VARCHAR(255) NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS products (
    id CHAR(32) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    unit VARCHAR(100) DEFAULT 'item',
    price BIGINT NOT NULL,
    cost_price BIGINT NOT NULL,
    valuation_method VARCHAR(10) DEFAULT 'AVG',
    barcode VARCHAR(255) NULL,
    description TEXT NULL,
    image_path VARCHAR(512) NULL,
    jenis_produk VARCHAR(32) NOT NULL DEFAULT 'Finished',
    stock BIGINT NOT NULL DEFAULT 0,
    min_stock BIGINT DEFAULT 0,
    discount BIGINT DEFAULT 0,
    tax BIGINT DEFAULT 0,
    is_deleted TINYINT DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE KEY uniq_products_barcode (barcode)
);

CREATE TABLE IF NOT EXISTS invoices (
    id CHAR(32) PRIMARY KEY,
    code VARCHAR(255) NOT NULL,
    customer VARCHAR(255) NULL,
    contact VARCHAR(255) NULL,
    address TEXT NULL,
    payment_method VARCHAR(32) DEFAULT 'cash',
    discount_type VARCHAR(32) DEFAULT 'percent',
    discount BIGINT DEFAULT 0,
    tax BIGINT DEFAULT 0,
    total_amount BIGINT DEFAULT 0,
    status VARCHAR(32) DEFAULT 'UNPAID',
    due_date DATETIME NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE KEY uniq_invoices_code (code)
);

CREATE TABLE IF NOT EXISTS invoice_installments (
    id CHAR(32) PRIMARY KEY,
    invoice_id CHAR(32) NOT NULL,
    installment_number INT NOT NULL,
    due_date DATETIME NOT NULL,
    amount BIGINT NOT NULL,
    paid_amount BIGINT DEFAULT 0,
    status VARCHAR(32) DEFAULT 'UNPAID',
    paid_at DATETIME NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS invoice_items (
    id CHAR(32) PRIMARY KEY,
    invoice_id CHAR(32) NOT NULL,
    product_id CHAR(32) NULL,
    product_name VARCHAR(255) NOT NULL,
    product_barcode VARCHAR(255) NULL,
    qty BIGINT NOT NULL,
    price BIGINT NOT NULL,
    cost_price BIGINT NOT NULL,
    profit BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    INDEX idx_invoice_id (invoice_id),
    INDEX idx_product_id (product_id)
);

CREATE TABLE IF NOT EXISTS invoice_payments (
    id CHAR(32) PRIMARY KEY,
    invoice_id CHAR(32) NOT NULL,
    amount BIGINT NOT NULL,
    method VARCHAR(100) NULL,
    note TEXT NULL,
    type VARCHAR(32) DEFAULT 'IN',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    INDEX idx_invoice_payment (invoice_id)
);

CREATE TABLE IF NOT EXISTS stock_movements (
    id CHAR(32) PRIMARY KEY,
    product_id CHAR(32) NOT NULL,
    type VARCHAR(32) NOT NULL,
    qty BIGINT NOT NULL,
    purchase_price BIGINT DEFAULT 0,
    remaining_qty BIGINT DEFAULT 0,
    note TEXT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    INDEX idx_stock_product (product_id)
);

CREATE TABLE IF NOT EXISTS expenses (
    id CHAR(32) PRIMARY KEY,
    name VARCHAR(255) NULL,
    amount BIGINT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS cash_flows (
    id CHAR(32) PRIMARY KEY,
    type VARCHAR(32) NOT NULL,
    source VARCHAR(100) NULL,
    reference_id CHAR(32) NULL,
    amount BIGINT NOT NULL,
    note TEXT NULL,
    created_at DATETIME NOT NULL
);

DROP TABLE IF EXISTS sync_changes;
DROP TABLE IF EXISTS sync_lock;
DROP TABLE IF EXISTS sync_metadata;
