ALTER TABLE users
MODIFY role ENUM(
    'admin',
    'guru',
    'staff',
    'siswa',
    'user',
    'owner',
    'cashier',
    'warehouse',
    'courier'
) NOT NULL DEFAULT 'user';

UPDATE users SET role = 'owner' WHERE role = 'admin';
UPDATE users
SET role = 'cashier'
WHERE role NOT IN ('owner', 'cashier', 'warehouse', 'courier');

ALTER TABLE users
MODIFY role ENUM('owner', 'cashier', 'warehouse', 'courier')
NOT NULL DEFAULT 'cashier';
