package db

import (
	"fmt"
)

func CleanupTenant(dbName string, tenantID string) {

	// drop database tenant
	Master.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))

	Master.Exec("DELETE FROM users_index WHERE tenant_id = ?", tenantID)

	// hapus dari master tenants
	Master.Exec("DELETE FROM tenants WHERE id = ?", tenantID)
}
