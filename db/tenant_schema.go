package db

import (
	"database/sql"
	_ "embed"
	"strings"
)

//go:embed tenant_schema.sql
var tenantSchema string

func RunTenantSchema(tenantDB *sql.DB) error {
	for _, statement := range strings.Split(tenantSchema, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := tenantDB.Exec(statement); err != nil {
			return err
		}
	}
	if err := ensureProductMetadataColumns(tenantDB); err != nil {
		return err
	}
	return dropLegacyNotificationColumns(tenantDB)
}

func ensureProductMetadataColumns(tenantDB *sql.DB) error {
	columns := []struct {
		name       string
		definition string
	}{
		{name: "description", definition: "TEXT NULL"},
		{name: "image_path", definition: "VARCHAR(512) NULL"},
	}
	for _, column := range columns {
		var exists bool
		if err := tenantDB.QueryRow(`
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = DATABASE()
				  AND table_name = 'products'
				  AND column_name = ?
			)
		`, column.name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := tenantDB.Exec("ALTER TABLE products ADD COLUMN `" + column.name + "` " + column.definition); err != nil {
			return err
		}
	}
	return nil
}

func dropLegacyNotificationColumns(tenantDB *sql.DB) error {
	legacyColumns := []struct {
		table  string
		column string
	}{
		{table: "products", column: "notified"},
		{table: "products", column: "notified_at"},
		{table: "products", column: "notified_type"},
		{table: "invoices", column: "notified"},
		{table: "invoices", column: "notified_at"},
		{table: "invoices", column: "notified_type"},
		{table: "invoice_installments", column: "notified"},
		{table: "invoice_installments", column: "notified_at"},
		{table: "invoice_installments", column: "notified_type"},
	}
	for _, legacy := range legacyColumns {
		var exists bool
		if err := tenantDB.QueryRow(`
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = DATABASE()
				  AND table_name = ?
				  AND column_name = ?
			)
		`, legacy.table, legacy.column).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tenantDB.Exec(
			"ALTER TABLE `" + legacy.table + "` DROP COLUMN `" + legacy.column + "`",
		); err != nil {
			return err
		}
	}
	return nil
}
