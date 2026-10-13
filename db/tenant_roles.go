package db

import "database/sql"

func MigrateTenantRoles(tenantDB *sql.DB) error {
	queries := []string{
		`ALTER TABLE users MODIFY phone VARCHAR(20) NULL`,
		`ALTER TABLE users
		MODIFY role ENUM(
			'admin',
			'guru',
			'staff',
			'siswa',
			'user',
			'owner',
			'cashier',
			'warehouse',
			'courier',
			'finance'
		) NOT NULL DEFAULT 'user'`,
		`UPDATE users SET role = 'owner' WHERE role = 'admin'`,
		`UPDATE users
		SET role = 'cashier'
		WHERE role NOT IN ('owner', 'cashier', 'warehouse', 'courier', 'finance')`,
		`ALTER TABLE users
		MODIFY role ENUM('owner', 'cashier', 'warehouse', 'courier', 'finance')
		NOT NULL DEFAULT 'cashier'`,
	}

	for _, query := range queries {
		if _, err := tenantDB.Exec(query); err != nil {
			return err
		}
	}
	return nil
}
