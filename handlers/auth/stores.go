package auth

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"assistant/db"
	"assistant/utils"

	"github.com/gin-gonic/gin"
)

type storeSummary struct {
	ID        string `json:"id"`
	Code      string `json:"code,omitempty"`
	Name      string `json:"name"`
	OwnerName string `json:"owner_name,omitempty"`
	Role      string `json:"role"`
	MaxUsers  int    `json:"max_users,omitempty"`
}

func Me(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	tenantID := c.GetString("tenant_id")
	if tenantID == "" {
		var account struct {
			Name  string `json:"name"`
			Email string `json:"email"`
			Phone string `json:"phone"`
		}
		err := db.Master.QueryRow(
			"SELECT name, email, COALESCE(phone, '') FROM accounts WHERE id = ?",
			userID,
		).Scan(&account.Name, &account.Email, &account.Phone)
		if err == sql.ErrNoRows {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Account not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account"})
			return
		}
		subscription, err := readSubscription(userID, time.Now())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account subscription"})
			return
		}
		ownedStores, err := countOwnedStores(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count owned stores"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"id":               userID,
			"name":             account.Name,
			"email":            account.Email,
			"phone":            account.Phone,
			"role":             "account",
			"tenant_id":        "",
			"tenant":           nil,
			"can_create_store": ownedStores < subscription.MaxStores,
		})
		return
	}

	var user struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	err := getStoreDB(tenantID, func(tenantDB *sql.DB) error {
		return tenantDB.QueryRow(`
			SELECT name, email, role
			FROM users
			WHERE id = ? AND tenant_id = ?
		`, userID, tenantID).Scan(&user.Name, &user.Email, &user.Role)
	})
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Account not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account"})
		}
		return
	}
	var account struct {
		Name  string
		Email string
		Phone string
	}
	accountErr := db.Master.QueryRow(
		"SELECT name, email, COALESCE(phone, '') FROM accounts WHERE id = ?",
		userID,
	).Scan(&account.Name, &account.Email, &account.Phone)
	if accountErr != nil && !errors.Is(accountErr, sql.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account"})
		return
	}
	if errors.Is(accountErr, sql.ErrNoRows) {
		account.Name = user.Name
		account.Email = user.Email
	}
	if !ensureLegacyTenantOwners(userID) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare store ownership"})
		return
	}

	var storeName string
	var storeCode string
	var maxUsers int
	var plan string
	var ownerAccountID string
	if err := db.Master.QueryRow(
		"SELECT name, code, max_users, plan, owner_account_id FROM tenants WHERE id = ?",
		tenantID,
	).Scan(&storeName, &storeCode, &maxUsers, &plan, &ownerAccountID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store"})
		return
	}

	role := normalizeRole(user.Role)
	subscription, err := readSubscription(ownerAccountID, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load subscription"})
		return
	}
	maxUsers = subscription.MaxUsers
	plan = subscription.Plan
	accountSubscription, err := readSubscription(userID, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account subscription"})
		return
	}
	ownedStores, err := countOwnedStores(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count owned stores"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":               userID,
		"name":             account.Name,
		"email":            account.Email,
		"phone":            account.Phone,
		"role":             role,
		"tenant_id":        tenantID,
		"tenant":           storeSummary{ID: tenantID, Code: storeCode, Name: storeName, Role: role, MaxUsers: maxUsers},
		"plan":             plan,
		"can_create_store": ownedStores < accountSubscription.MaxStores,
	})
}

func ListStores(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	if !ensureLegacyTenantOwners(userID) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare store ownership"})
		return
	}
	subscription, err := readSubscription(userID, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load subscription"})
		return
	}

	rows, err := db.Master.Query(`
		SELECT ui.tenant_id, t.code, t.name, t.max_users, t.db_name, t.owner_account_id
		FROM users_index ui
		JOIN tenants t ON t.id = ui.tenant_id
		WHERE ui.user_id = ? AND t.status = 'active'
		ORDER BY t.created_at ASC
	`, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load stores"})
		return
	}
	defer rows.Close()

	stores := make([]storeSummary, 0)
	for rows.Next() {
		var candidate storeSummary
		var dbName string
		var ownerAccountID sql.NullString
		if err := rows.Scan(&candidate.ID, &candidate.Code, &candidate.Name, &candidate.MaxUsers, &dbName, &ownerAccountID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read stores"})
			return
		}

		tenantDB, err := db.GetTenantDB(dbName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Tenant database connection failed"})
			return
		}
		var role string
		err = tenantDB.QueryRow(
			"SELECT role FROM users WHERE id = ? AND tenant_id = ?",
			userID,
			candidate.ID,
		).Scan(&role)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read store account"})
			return
		}
		candidate.Role = normalizeRole(role)
		if candidate.Role == "owner" {
			candidate.MaxUsers = subscription.MaxUsers
		} else if ownerAccountID.Valid {
			storeSubscription, err := readSubscription(ownerAccountID.String, time.Now())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store subscription"})
				return
			}
			candidate.MaxUsers = storeSubscription.MaxUsers
		}
		var ownerName sql.NullString
		if err := tenantDB.QueryRow(`
			SELECT name FROM users
			WHERE tenant_id = ? AND role IN ('owner', 'admin')
			ORDER BY CASE WHEN role = 'owner' THEN 0 ELSE 1 END
			LIMIT 1
		`, candidate.ID).Scan(&ownerName); err != nil && err != sql.ErrNoRows {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read store owner"})
			return
		}
		if ownerName.Valid {
			candidate.OwnerName = ownerName.String
		}
		stores = append(stores, candidate)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read stores"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"stores": stores})
}

func CreateStore(c *gin.Context) {
	var req struct {
		TenantName string `json:"tenant_name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.TenantName = strings.TrimSpace(req.TenantName)
	if req.TenantName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Store name is required"})
		return
	}

	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	var user ownerAccount
	err := db.Master.QueryRow(`
		SELECT id, name, email, password_hash
		FROM accounts WHERE id = ?
	`, userID).Scan(&user.ID, &user.Name, &user.Email, &user.PasswordHash)
	if err == sql.ErrNoRows && tenantID != "" {
		var ok bool
		user, ok = currentOwner(c)
		if !ok {
			return
		}
		if err := ensureAccount(user); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare account for store"})
			return
		}
	} else if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Account not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account"})
		return
	}
	if !ensureLegacyTenantOwners(userID) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare store ownership"})
		return
	}

	storeLimitTx, err := db.Master.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start store limit transaction"})
		return
	}
	defer storeLimitTx.Rollback()
	var plan string
	var subscriptionEnd sql.NullTime
	if err := storeLimitTx.QueryRow(
		"SELECT subscription_plan, subscription_end FROM accounts WHERE id = ? FOR UPDATE",
		userID,
	).Scan(&plan, &subscriptionEnd); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify account subscription"})
		return
	}
	premiumActive := plan == "premium" && subscriptionEnd.Valid && subscriptionEnd.Time.After(time.Now())
	maxStores := 1
	if premiumActive {
		maxStores = premiumMaxStores
	}
	var ownedStores int
	if err := storeLimitTx.QueryRow(
		"SELECT COUNT(*) FROM tenants WHERE owner_account_id = ? AND status IN ('active', 'provisioning')",
		userID,
	).Scan(&ownedStores); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count owned stores"})
		return
	}
	if ownedStores >= maxStores {
		message := "Paket Free hanya mengizinkan satu toko. Upgrade ke Starter Pack untuk membuat lebih banyak toko."
		if premiumActive {
			message = "Akun ini sudah mencapai batas maksimal 3 toko Starter Pack."
		}
		c.JSON(http.StatusForbidden, gin.H{"error": message})
		return
	}
	tenant, userID, err := provisionTenantForAccount(
		req.TenantName, user.Name, user.Email, "", user.PasswordHash, userID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store creation failed", "detail": err.Error()})
		return
	}
	if err := storeLimitTx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Store was created but the account limit transaction could not be committed"})
		return
	}

	token, err := utils.GenerateJWT(userID, tenant.ID, "owner")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token generation failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":     "Store created",
		"token":       token,
		"name":        user.Name,
		"role":        "owner",
		"tenant_id":   tenant.ID,
		"tenant_code": tenant.Code,
		"tenant":      storeSummary{ID: tenant.ID, Code: tenant.Code, Name: tenant.Name, Role: "owner"},
	})
}

func activePaidPlan(plan string, subscriptionEnd sql.NullTime, now time.Time) bool {
	switch plan {
	case "month", "quarter", "year":
		return subscriptionEnd.Valid && subscriptionEnd.Time.After(now)
	default:
		return false
	}
}

func RenameStore(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Store name must be between 1 and 100 characters"})
		return
	}
	owner, ok := currentOwner(c)
	if !ok {
		return
	}
	result, err := db.Master.Exec(
		"UPDATE tenants SET name = ? WHERE id = ? AND status = 'active'",
		req.Name,
		owner.TenantID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update store name"})
		return
	}
	if affected, err := result.RowsAffected(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify store name update"})
		return
	} else if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Store was not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Store name updated", "name": req.Name})
}

func SwitchStore(c *gin.Context) {
	var req struct {
		TenantID string `json:"tenant_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	err := db.Master.QueryRow(`
		SELECT ui.user_id
		FROM users_index ui
		JOIN tenants t ON t.id = ui.tenant_id
		WHERE ui.tenant_id = ?
			AND ui.user_id = ?
			AND t.status = 'active'
	`, req.TenantID, userID).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Store is not available for this account"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find store"})
		}
		return
	}

	var member struct {
		Name string
		Role string
	}
	err = getStoreDB(req.TenantID, func(tenantDB *sql.DB) error {
		return tenantDB.QueryRow(
			"SELECT name, role FROM users WHERE id = ? AND tenant_id = ?",
			userID,
			req.TenantID,
		).Scan(&member.Name, &member.Role)
	})
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusForbidden, gin.H{"error": "Owner access is required for this store"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store account"})
		}
		return
	}
	role := normalizeRole(member.Role)
	token, err := utils.GenerateJWT(userID, req.TenantID, role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token generation failed"})
		return
	}

	var storeName string
	var storeCode string
	if err := db.Master.QueryRow("SELECT name, code FROM tenants WHERE id = ?", req.TenantID).Scan(&storeName, &storeCode); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load store"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   "Store switched",
		"token":     token,
		"name":      member.Name,
		"role":      role,
		"tenant_id": req.TenantID,
		"tenant":    storeSummary{ID: req.TenantID, Code: storeCode, Name: storeName, Role: role},
	})
}

func JoinStore(c *gin.Context) {
	var req struct {
		StoreID string `json:"store_id"`
		Code    string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	lookup := strings.TrimSpace(req.StoreID)
	if lookup == "" {
		lookup = strings.TrimSpace(req.Code)
	}
	lookup = strings.ToUpper(lookup)
	if lookup == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Store code or ID is required"})
		return
	}

	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	var account struct {
		Name         string
		Email        string
		PasswordHash string
	}
	err := db.Master.QueryRow(
		"SELECT name, email, password_hash FROM accounts WHERE id = ?",
		userID,
	).Scan(&account.Name, &account.Email, &account.PasswordHash)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Account not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account"})
		return
	}

	var tenant struct {
		ID     string
		Code   string
		Name   string
		DBName string
	}
	err = db.Master.QueryRow(`
		SELECT id, code, name, db_name
		FROM tenants
		WHERE (id = ? OR code = ?) AND status = 'active'
	`, lookup, lookup).Scan(&tenant.ID, &tenant.Code, &tenant.Name, &tenant.DBName)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Store code or ID was not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find store"})
		return
	}

	tenantDB, err := db.GetTenantDB(tenant.DBName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Tenant database connection failed"})
		return
	}
	var legacyPlan string
	var legacyStart, legacyEnd sql.NullTime
	if err := db.Master.QueryRow(
		"SELECT plan, subscription_start, subscription_end FROM tenants WHERE id = ?",
		tenant.ID,
	).Scan(&legacyPlan, &legacyStart, &legacyEnd); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read store plan"})
		return
	}
	if _, err := resolveLegacyTenantOwner(tenant.ID, tenant.DBName, legacyPlan, legacyStart, legacyEnd); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare store ownership"})
		return
	}

	token, err := utils.GenerateJWT(userID, tenant.ID, "cashier")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token generation failed"})
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
		premiumMaxUsers, freeMaxUsers, tenant.ID,
	).Scan(&maxUsers); err != nil {
		_ = tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read store membership limit"})
		return
	}
	var existingMembership string
	err = tx.QueryRow(
		"SELECT tenant_id FROM users_index WHERE user_id = ? AND tenant_id = ?",
		userID,
		tenant.ID,
	).Scan(&existingMembership)
	if err == nil {
		_ = tx.Rollback()
		c.JSON(http.StatusConflict, gin.H{"error": "Akun ini sudah menjadi anggota toko"})
		return
	}
	if err != sql.ErrNoRows {
		_ = tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check store membership"})
		return
	}
	var currentUsers int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM users_index WHERE tenant_id = ?",
		tenant.ID,
	).Scan(&currentUsers); err != nil {
		_ = tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count store members"})
		return
	}
	if currentUsers >= maxUsers {
		_ = tx.Rollback()
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Toko ini sudah mencapai batas akun untuk paket saat ini. Owner perlu upgrade paket agar akun dapat bergabung.",
		})
		return
	}

	if _, err := tenantDB.Exec(`
		INSERT INTO users (id, tenant_id, name, email, password_hash, role)
		VALUES (?, ?, ?, ?, ?, 'cashier')
	`, userID, tenant.ID, account.Name, account.Email, account.PasswordHash); err != nil {
		_ = tx.Rollback()
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			c.JSON(http.StatusConflict, gin.H{"error": "Email sudah digunakan di toko ini"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add account to store"})
		return
	}
	if _, err := tx.Exec(
		"INSERT INTO users_index (user_id, email, tenant_id) VALUES (?, ?, ?)",
		userID,
		account.Email,
		tenant.ID,
	); err != nil {
		_ = tx.Rollback()
		if _, cleanupErr := tenantDB.Exec(
			"DELETE FROM users WHERE id = ? AND tenant_id = ?",
			userID,
			tenant.ID,
		); cleanupErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Membership registration and cleanup failed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register store membership"})
		return
	}
	if err := tx.Commit(); err != nil {
		if _, cleanupErr := tenantDB.Exec(
			"DELETE FROM users WHERE id = ? AND tenant_id = ?",
			userID,
			tenant.ID,
		); cleanupErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Membership commit failed and tenant account cleanup failed"})
			return
		}
		if _, cleanupErr := db.Master.Exec(
			"DELETE FROM users_index WHERE user_id = ? AND tenant_id = ?",
			userID,
			tenant.ID,
		); cleanupErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Membership commit failed and index cleanup failed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit store membership"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":     "Joined store",
		"name":        account.Name,
		"role":        "cashier",
		"token":       token,
		"tenant_id":   tenant.ID,
		"tenant_code": tenant.Code,
		"tenant":      storeSummary{ID: tenant.ID, Code: tenant.Code, Name: tenant.Name, Role: "cashier"},
	})
}

func ensureAccount(user ownerAccount) error {
	_, err := db.Master.Exec(`
		INSERT INTO accounts (id, name, email, password_hash)
		VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE name = VALUES(name)
	`, user.ID, user.Name, user.Email, user.PasswordHash)
	return err
}

type ownerAccount struct {
	ID           string
	TenantID     string
	Name         string
	Email        string
	PasswordHash string
	Role         string
}

func currentOwner(c *gin.Context) (ownerAccount, bool) {
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")
	if userID == "" || tenantID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return ownerAccount{}, false
	}

	var user ownerAccount
	err := getStoreDB(tenantID, func(tenantDB *sql.DB) error {
		return tenantDB.QueryRow(`
			SELECT id, tenant_id, name, email, password_hash, role
			FROM users
			WHERE id = ? AND tenant_id = ?
		`, userID, tenantID).Scan(
			&user.ID,
			&user.TenantID,
			&user.Name,
			&user.Email,
			&user.PasswordHash,
			&user.Role,
		)
	})
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Account not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account"})
		}
		return ownerAccount{}, false
	}
	if normalizeRole(user.Role) != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Owner access is required"})
		return ownerAccount{}, false
	}
	return user, true
}
