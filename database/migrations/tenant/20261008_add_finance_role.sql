ALTER TABLE users
MODIFY role ENUM(
    'owner',
    'cashier',
    'warehouse',
    'courier',
    'finance'
) NOT NULL DEFAULT 'cashier';
