package auth

import (
	"database/sql"
	"log"
	"net/http"
	"strings"

	"assistant/db"
	"assistant/utils"

	"github.com/gin-gonic/gin"
)

type storeMember struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	Role  string `json:"role"`
}

func ListStoreMembers(c *gin.Context) {
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")
	if userID == "" || tenantID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	tenantDB, err := tenantDatabaseForID(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Tenant database connection failed"})
		return
	}
	var memberExists bool
	if err := tenantDB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND tenant_id = ?)",
		userID,
		tenantID,
	).Scan(&memberExists); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify store membership"})
		return
	}
	if !memberExists {
		c.JSON(http.StatusForbidden, gin.H{"error": "Store membership is required"})
		return
	}
	rows, err := tenantDB.Query(`
		SELECT id, name, email, COALESCE(phone, ''), role
		FROM users
		WHERE tenant_id = ?
		ORDER BY FIELD(role, 'owner', 'cashier', 'warehouse', 'courier', 'finance'), name
	`, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list store members"})
		return
	}
	defer rows.Close()

	members := make([]storeMember, 0)
	for rows.Next() {
		var member storeMember
		if err := rows.Scan(&member.ID, &member.Name, &member.Email, &member.Phone, &member.Role); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read store member"})
			return
		}
		member.Role = normalizeRole(member.Role)
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list store members"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"members": members})
}

func AddStoreMember(c *gin.Context) {
	var req struct {
		Name     string `json:"name" binding:"required"`
		Email    string `json:"email" binding:"required,email"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
		Role     string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Phone = strings.TrimSpace(req.Phone)
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	if req.Name == "" || len([]rune(req.Name)) > 100 || len(req.Phone) > 20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required and phone must be at most 20 characters"})
		return
	}
	if req.Role != "cashier" && req.Role != "warehouse" && req.Role != "finance" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role must be cashier, warehouse, or finance"})
		return
	}

	owner, ok := currentOwner(c)
	if !ok {
		return
	}
	if !ensureLegacyTenantOwners(owner.ID) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare store ownership"})
		return
	}
	tenantDB, err := tenantDatabaseForID(owner.TenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Tenant database connection failed"})
		return
	}
	tx, err := db.Master.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start membership transaction"})
		return
	}
	var maxUsers int
	if err := tx.QueryRow(
		`SELECT CASE
			WHEN a.subscription_plan = 'premium' AND a.subscription_end > CURRENT_TIMESTAMP THEN ?
			ELSE ?
		END
		FROM tenants t
		JOIN accounts a ON a.id = t.owner_account_id
		WHERE t.id = ? AND t.status = 'active'
		FOR UPDATE`,
		premiumMaxUsers, freeMaxUsers, owner.TenantID,
	).Scan(&maxUsers); err != nil {
		_ = tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read store membership limit"})
		return
	}

	var currentUsers int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM users_index WHERE tenant_id = ?",
		owner.TenantID,
	).Scan(&currentUsers); err != nil {
		_ = tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count store members"})
		return
	}
	if currentUsers >= maxUsers {
		_ = tx.Rollback()
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Toko ini sudah mencapai batas akun untuk paket saat ini. Owner perlu upgrade paket agar dapat menambahkan anggota.",
		})
		return
	}
	if maxUsers == freeMaxUsers {
		var roleCount int
		if err := tenantDB.QueryRow(
			"SELECT COUNT(*) FROM users WHERE tenant_id = ? AND role = ?",
			owner.TenantID,
			req.Role,
		).Scan(&roleCount); err != nil {
			_ = tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check role availability"})
			return
		}
		if roleCount > 0 {
			_ = tx.Rollback()
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Paket Free hanya menyediakan satu akun untuk setiap role. Upgrade ke Starter Pack untuk menambahkan akun dengan role ini.",
			})
			return
		}
	}

	var userID, name, passwordHash string
	newAccount := false
	err = tx.QueryRow(
		"SELECT id, name, password_hash FROM accounts WHERE email = ?",
		req.Email,
	).Scan(&userID, &name, &passwordHash)
	if err == sql.ErrNoRows {
		if len(req.Password) < 8 {
			_ = tx.Rollback()
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password for a new account must be at least 8 characters"})
			return
		}
		userID = utils.NewULID()
		name = req.Name
		passwordHash, err = utils.HashPassword(req.Password)
		if err != nil {
			_ = tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Password hash failed"})
			return
		}
		newAccount = true
	} else if err != nil {
		_ = tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find account"})
		return
	}

	var existingMembership string
	err = tx.QueryRow(
		"SELECT tenant_id FROM users_index WHERE user_id = ? AND tenant_id = ?",
		userID,
		owner.TenantID,
	).Scan(&existingMembership)
	if err == nil {
		_ = tx.Rollback()
		c.JSON(http.StatusConflict, gin.H{"error": "This account is already a member of the store"})
		return
	}
	if err != sql.ErrNoRows {
		_ = tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check store membership"})
		return
	}

	if _, err := tenantDB.Exec(`
		INSERT INTO users (id, tenant_id, name, email, phone, password_hash, role)
		VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?)
	`, userID, owner.TenantID, name, req.Email, req.Phone, passwordHash, req.Role); err != nil {
		_ = tx.Rollback()
		c.JSON(http.StatusConflict, gin.H{"error": "Failed to add account to this store"})
		return
	}
	if newAccount {
		if _, err := tx.Exec(
			"INSERT INTO accounts (id, name, email, phone, password_hash) VALUES (?, ?, ?, NULLIF(?, ''), ?)",
			userID,
			name,
			req.Email,
			req.Phone,
			passwordHash,
		); err != nil {
			_ = tx.Rollback()
			cleanupTenantMember(tenantDB, userID, owner.TenantID)
			c.JSON(http.StatusConflict, gin.H{"error": "An account with this email already exists"})
			return
		}
	}
	if _, err := tx.Exec(
		"INSERT INTO users_index (user_id, email, phone, tenant_id) VALUES (?, ?, NULLIF(?, ''), ?)",
		userID,
		req.Email,
		req.Phone,
		owner.TenantID,
	); err != nil {
		_ = tx.Rollback()
		cleanupTenantMember(tenantDB, userID, owner.TenantID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create store membership"})
		return
	}
	if err := tx.Commit(); err != nil {
		cleanupTenantMember(tenantDB, userID, owner.TenantID)
		if _, cleanupErr := db.Master.Exec(
			"DELETE FROM users_index WHERE user_id = ? AND tenant_id = ?",
			userID,
			owner.TenantID,
		); cleanupErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Membership commit failed and index cleanup failed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit store membership"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Member added",
		"member":  storeMember{ID: userID, Name: name, Email: req.Email, Phone: req.Phone, Role: req.Role},
	})
}

func UpdateStoreMember(c *gin.Context) {
	owner, ok := currentOwner(c)
	if !ok {
		return
	}
	userID := strings.TrimSpace(c.Param("userID"))
	if userID == "" || userID == owner.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "A valid employee account ID is required"})
		return
	}
	var req struct {
		Name  string `json:"name" binding:"required"`
		Phone string `json:"phone"`
		Role  string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid employee details"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Phone = strings.TrimSpace(req.Phone)
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	if req.Name == "" || len([]rune(req.Name)) > 100 || len(req.Phone) > 20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name is required and phone must be at most 20 characters"})
		return
	}
	if req.Role != "cashier" && req.Role != "warehouse" && req.Role != "finance" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role must be cashier, warehouse, or finance"})
		return
	}
	tenantDB, err := tenantDatabaseForID(owner.TenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Tenant database connection failed"})
		return
	}
	var currentRole string
	if err := tenantDB.QueryRow(
		"SELECT role FROM users WHERE id = ? AND tenant_id = ?",
		userID,
		owner.TenantID,
	).Scan(&currentRole); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Store member was not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store member"})
		}
		return
	}
	if normalizeRole(currentRole) == "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "The store owner cannot be edited as an employee"})
		return
	}
	maxUsers, err := effectiveStoreMaxUsers(owner.TenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read store membership limit"})
		return
	}
	if maxUsers == freeMaxUsers && req.Role != normalizeRole(currentRole) {
		var roleCount int
		if err := tenantDB.QueryRow(
			"SELECT COUNT(*) FROM users WHERE tenant_id = ? AND role = ? AND id <> ?",
			owner.TenantID,
			req.Role,
			userID,
		).Scan(&roleCount); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check role availability"})
			return
		}
		if roleCount > 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "Paket Free hanya menyediakan satu akun untuk setiap role."})
			return
		}
	}
	result, err := tenantDB.Exec(`
		UPDATE users SET name = ?, phone = NULLIF(?, ''), role = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND tenant_id = ?
	`, req.Name, req.Phone, req.Role, userID, owner.TenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update store member"})
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify store member update"})
		return
	}
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Store member was not found"})
		return
	}
	if _, err := db.Master.Exec(`
		UPDATE users_index SET phone = ?
		WHERE user_id = ? AND tenant_id = ?
	`, nullablePhone(req.Phone), userID, owner.TenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Employee updated, but store membership phone could not be synchronized"})
		return
	}
	var email string
	if err := tenantDB.QueryRow(
		"SELECT email FROM users WHERE id = ? AND tenant_id = ?",
		userID,
		owner.TenantID,
	).Scan(&email); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Employee updated, but employee details could not be reloaded"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"member": storeMember{ID: userID, Name: req.Name, Email: email, Phone: req.Phone, Role: req.Role}})
}

func nullablePhone(phone string) any {
	if phone == "" {
		return nil
	}
	return phone
}

func RemoveStoreMember(c *gin.Context) {
	owner, ok := currentOwner(c)
	if !ok {
		return
	}
	userID := strings.TrimSpace(c.Param("userID"))
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Member account ID is required"})
		return
	}
	if userID == owner.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The store owner cannot remove their own membership"})
		return
	}

	tenantDB, err := tenantDatabaseForID(owner.TenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Tenant database connection failed"})
		return
	}
	var memberRole string
	if err := tenantDB.QueryRow(
		"SELECT role FROM users WHERE id = ? AND tenant_id = ?",
		userID,
		owner.TenantID,
	).Scan(&memberRole); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Store member was not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store member"})
		}
		return
	}
	if normalizeRole(memberRole) == "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "The store owner cannot be removed"})
		return
	}

	// Remove tenant credentials first so an existing JWT stops working immediately.
	if _, err := tenantDB.Exec(
		"DELETE FROM users WHERE id = ? AND tenant_id = ?",
		userID,
		owner.TenantID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove member from store"})
		return
	}
	_, err = db.Master.Exec(
		"DELETE FROM users_index WHERE user_id = ? AND tenant_id = ?",
		userID,
		owner.TenantID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Member access was revoked, but store membership cleanup failed",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Member removed from store"})
}

func tenantDatabaseForID(tenantID string) (*sql.DB, error) {
	var dbName string
	if err := db.Master.QueryRow(
		"SELECT db_name FROM tenants WHERE id = ? AND status = 'active'",
		tenantID,
	).Scan(&dbName); err != nil {
		return nil, err
	}
	return db.GetTenantDB(dbName)
}

func cleanupTenantMember(tenantDB *sql.DB, userID, tenantID string) {
	if _, err := tenantDB.Exec("DELETE FROM users WHERE id = ? AND tenant_id = ?", userID, tenantID); err != nil {
		log.Printf("tenant member cleanup failed for account %s in tenant %s: %v", userID, tenantID, err)
	}
}
