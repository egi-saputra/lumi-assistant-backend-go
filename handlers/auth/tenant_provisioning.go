package auth

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"assistant/db"
	"assistant/utils"

	mysql "github.com/go-sql-driver/mysql"
)

type provisionedTenant struct {
	ID     string
	Code   string
	Name   string
	DBName string
}

var tenantNameSeparators = regexp.MustCompile(`[^a-z0-9]+`)

func provisionTenant(tenantName, name, email, phone, passwordHash string) (provisionedTenant, string, error) {
	return provisionTenantForAccount(
		tenantName,
		name,
		email,
		phone,
		passwordHash,
		utils.NewULID(),
	)
}

func provisionTenantForAccount(tenantName, name, email, phone, passwordHash, userID string) (provisionedTenant, string, error) {
	tenant := provisionedTenant{
		ID:   utils.NewULID(),
		Name: tenantName,
	}
	tenant.DBName = tenantDatabaseName(tenant.Name, tenant.ID)

	var err error
	for attempt := 0; attempt < 10; attempt++ {
		tenant.Code, err = utils.GenerateTenantCode()
		if err != nil {
			return provisionedTenant{}, "", err
		}
		_, err = db.Master.Exec(`
			INSERT INTO tenants (id, name, code, db_name, owner_account_id, status)
			VALUES (?, ?, ?, ?, ?, 'provisioning')
		`, tenant.ID, tenant.Name, tenant.Code, tenant.DBName, userID)
		if err == nil {
			break
		}
		if mysqlErr, ok := err.(*mysql.MySQLError); !ok || mysqlErr.Number != 1062 {
			return provisionedTenant{}, "", fmt.Errorf("insert tenant: %w", err)
		}
	}
	if err != nil {
		return provisionedTenant{}, "", fmt.Errorf("generate a unique tenant code after 10 attempts: %w", err)
	}

	_, err = db.Master.Exec("CREATE DATABASE `" + tenant.DBName + "`")
	if err != nil {
		cleanupTenant(tenant.DBName, tenant.ID)
		return provisionedTenant{}, "", fmt.Errorf("create tenant database: %w", err)
	}

	tenantDB, err := db.GetTenantDB(tenant.DBName)
	if err != nil {
		cleanupTenant(tenant.DBName, tenant.ID)
		return provisionedTenant{}, "", fmt.Errorf("initialize tenant database: %w", err)
	}

	_, err = tenantDB.Exec(`
		INSERT INTO users (id, name, email, phone, password_hash, role, tenant_id)
		VALUES (?, ?, ?, NULLIF(?, ''), ?, 'owner', ?)
	`, userID, name, email, phone, passwordHash, tenant.ID)
	if err != nil {
		cleanupTenant(tenant.DBName, tenant.ID)
		return provisionedTenant{}, "", fmt.Errorf("create owner: %w", err)
	}

	_, err = db.Master.Exec(`
		INSERT INTO users_index (user_id, email, phone, tenant_id)
		VALUES (?, ?, NULLIF(?, ''), ?)
	`, userID, email, phone, tenant.ID)
	if err != nil {
		cleanupTenant(tenant.DBName, tenant.ID)
		return provisionedTenant{}, "", fmt.Errorf("index owner: %w", err)
	}

	_, err = db.Master.Exec(`
		UPDATE tenants SET status = 'active'
		WHERE id = ?
	`, tenant.ID)
	if err != nil {
		cleanupTenant(tenant.DBName, tenant.ID)
		return provisionedTenant{}, "", fmt.Errorf("activate tenant: %w", err)
	}

	return tenant, userID, nil
}

func tenantDatabaseName(name, tenantID string) string {
	slug := strings.Trim(tenantNameSeparators.ReplaceAllString(strings.ToLower(name), "_"), "_")
	if slug == "" {
		slug = "store"
	}
	if len(slug) > 48 {
		slug = strings.TrimRight(slug[:48], "_")
	}
	suffix := strings.ToLower(tenantID)
	if len(suffix) > 10 {
		suffix = suffix[len(suffix)-10:]
	}
	return "toko_" + slug + "_" + suffix
}

func cleanupTenant(dbName, tenantID string) {
	db.CloseTenantDB(dbName)
	if _, err := db.Master.Exec("DROP DATABASE IF EXISTS `" + dbName + "`"); err != nil {
		log.Printf("tenant cleanup failed to drop database %s: %v", dbName, err)
	}
	if _, err := db.Master.Exec("DELETE FROM users_index WHERE tenant_id = ?", tenantID); err != nil {
		log.Printf("tenant cleanup failed to remove user index for %s: %v", tenantID, err)
	}
	if _, err := db.Master.Exec("DELETE FROM tenants WHERE id = ?", tenantID); err != nil {
		log.Printf("tenant cleanup failed to remove tenant %s: %v", tenantID, err)
	}
}
