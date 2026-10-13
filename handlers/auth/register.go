package auth

import (
	"database/sql"
	"assistant/db"
	"assistant/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func Register(c *gin.Context) {

	var req struct {
		TenantID string `json:"tenant_id" binding:"required"` // ULID
		Name     string `json:"name" binding:"required"`
		Email    string `json:"email" binding:"required,email"`
		Phone    string `json:"phone"`
		Password string `json:"password" binding:"required,min=6"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	phone := utils.NormalizePhone(req.Phone)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	// =========================
	// 1. ambil db_name tenant
	// =========================
	var dbName, legacyPlan string
	var legacyStart, legacyEnd sql.NullTime
	err := db.Master.QueryRow(`
		SELECT db_name, plan, subscription_start, subscription_end FROM tenants
		WHERE id = ? AND status = 'active'
	`, req.TenantID).Scan(&dbName, &legacyPlan, &legacyStart, &legacyEnd)

	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid tenant"})
		return
	}

	var (
		currentUsers int
		maxUsers     int
	)

	// =========================
	// 2. connect tenant DB
	// =========================
	tenantDB, err := db.GetTenantDB(dbName)
	if err != nil {
		c.JSON(500, gin.H{"error": "DB connection failed"})
		return
	}
	if _, err := resolveLegacyTenantOwner(req.TenantID, dbName, legacyPlan, legacyStart, legacyEnd); err != nil {
		c.JSON(500, gin.H{"error": "failed to prepare store ownership"})
		return
	}
	maxUsers, err = effectiveStoreMaxUsers(req.TenantID)
	if err != nil {
		c.JSON(400, gin.H{"error": "tenant not found"})
		return
	}
	if err := db.Master.QueryRow(
		"SELECT COUNT(*) FROM users_index WHERE tenant_id = ?",
		req.TenantID,
	).Scan(&currentUsers); err != nil {
		c.JSON(500, gin.H{"error": "failed to count users"})
		return
	}
	if currentUsers >= maxUsers {
		c.JSON(403, gin.H{"error": "Kapasitas akun toko sudah mencapai batas paket saat ini."})
		return
	}

	// =========================
	// 3. cek email unik
	// =========================
	var exists string

	err = tenantDB.QueryRow(`
		SELECT id FROM users WHERE email = ?
	`, req.Email).Scan(&exists)

	if err == nil {
		c.JSON(409, gin.H{"error": "Email already registered"})
		return
	}
	if err != sql.ErrNoRows {
		c.JSON(500, gin.H{"error": "Database error"})
		return
	}

	// =========================
	// 4. cek phone unik
	// =========================
	err = tenantDB.QueryRow(`
		SELECT id FROM users WHERE phone = ?
	`, phone).Scan(&exists)

	if err == nil {
		c.JSON(409, gin.H{"error": "Phone already registered"})
		return
	}
	if err != sql.ErrNoRows {
		c.JSON(500, gin.H{"error": "Database error"})
		return
	}

	// =========================
	// 5. hash password
	// =========================
	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		c.JSON(500, gin.H{"error": "Password hash failed"})
		return
	}

	userID := utils.NewULID()

	// =========================
	// 6. insert user ke tenant DB
	// =========================
	_, err = tenantDB.Exec(`
		INSERT INTO users (
			id,
			tenant_id,
			name,
			email,
			phone,
			password_hash,
			role
		)
		VALUES (?, ?, ?, NULLIF(?, ''), ?, 'cashier')
	`,
		userID,
		req.TenantID,
		req.Name,
		req.Email,
		phone,
		hash,
	)

	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// =========================
	// 7. insert ke master users_index
	// =========================
	_, err = db.Master.Exec(`
		INSERT INTO users_index (user_id, email, phone, tenant_id)
		VALUES (?, ?, NULLIF(?, ''), ?)
	`, userID, req.Email, phone, req.TenantID)

	if err != nil {
		// rollback manual
		tenantDB.Exec("DELETE FROM users WHERE id = ?", userID)

		c.JSON(500, gin.H{"error": "Index sync failed"})
		return
	}

	// =========================
	// RESPONSE
	// =========================
	c.JSON(http.StatusCreated, gin.H{
		"message": "User created",
	})
}
