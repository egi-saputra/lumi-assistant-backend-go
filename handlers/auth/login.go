package auth

import (
	"database/sql"
	"net/http"
	"strings"

	"assistant/db"
	"assistant/utils"

	"github.com/gin-gonic/gin"
)

type storeOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`

	userID string
}

func Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
		TenantID string `json:"tenant_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "Validation failed",
			"errors":  utils.FormatValidationError(err),
		})
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	var account struct {
		ID           string
		Name         string
		Email        string
		PasswordHash string
	}
	err := db.Master.QueryRow(`
		SELECT id, name, email, password_hash
		FROM accounts
		WHERE email = ?
	`, req.Email).Scan(&account.ID, &account.Name, &account.Email, &account.PasswordHash)
	if err == sql.ErrNoRows {
		legacyLogin(c, req.Email, req.Password, req.TenantID)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find account"})
		return
	}
	if !utils.CheckPassword(req.Password, account.PasswordHash) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"message": "Authentication failed",
			"errors":  []string{"Email or password is incorrect"},
		})
		return
	}

	rows, err := db.Master.Query(`
		SELECT ui.tenant_id, t.name, t.db_name
		FROM users_index ui
		JOIN tenants t ON t.id = ui.tenant_id
		WHERE ui.user_id = ? AND t.status = 'active'
			AND (? = '' OR ui.tenant_id = ?)
		ORDER BY t.created_at ASC
	`, account.ID, req.TenantID, req.TenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find account stores"})
		return
	}

	stores := make([]storeOption, 0)
	for rows.Next() {
		var option storeOption
		var dbName string
		option.userID = account.ID
		if err := rows.Scan(&option.ID, &option.Name, &dbName); err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read account stores"})
			return
		}
		tenantDB, err := db.GetTenantDB(dbName)
		if err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Tenant database connection failed"})
			return
		}
		err = tenantDB.QueryRow(
			"SELECT role FROM users WHERE id = ? AND tenant_id = ?",
			account.ID,
			option.ID,
		).Scan(&option.Role)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read store membership"})
			return
		}
		option.Role = normalizeRole(option.Role)
		stores = append(stores, option)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read account stores"})
		return
	}
	rows.Close()

	if req.TenantID != "" && len(stores) == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "This account is not a member of the selected store"})
		return
	}
	if req.TenantID == "" && len(stores) > 1 {
		c.JSON(http.StatusOK, gin.H{"message": "Choose a store", "stores": stores})
		return
	}
	if len(stores) == 0 {
		token, err := utils.GenerateJWT(account.ID, "", "account")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Token generation failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":   "Login successful",
			"name":      account.Name,
			"role":      "account",
			"token":     token,
			"tenant_id": "",
			"stores":    stores,
		})
		return
	}

	selected := stores[0]
	token, err := utils.GenerateJWT(account.ID, selected.ID, selected.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token generation failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   "Login successful",
		"name":      account.Name,
		"role":      selected.Role,
		"token":     token,
		"tenant_id": selected.ID,
		"tenant":    gin.H{"id": selected.ID, "name": selected.Name},
		"stores":    stores,
	})
}

func legacyLogin(c *gin.Context, email, password, tenantID string) {
	rows, err := db.Master.Query(`
		SELECT ui.user_id, ui.tenant_id, t.name, t.db_name
		FROM users_index ui
		JOIN tenants t ON t.id = ui.tenant_id
		WHERE ui.email = ?
			AND t.status = 'active'
			AND (? = '' OR ui.tenant_id = ?)
		ORDER BY t.created_at ASC
	`, email, tenantID, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find account"})
		return
	}
	defer rows.Close()

	stores := make([]storeOption, 0)
	for rows.Next() {
		var option storeOption
		var dbName string
		if err := rows.Scan(&option.userID, &option.ID, &option.Name, &dbName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read account"})
			return
		}

		tenantDB, err := db.GetTenantDB(dbName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Tenant database connection failed"})
			return
		}

		var passwordHash, role string
		err = tenantDB.QueryRow(`
			SELECT password_hash, role
			FROM users
			WHERE id = ? AND tenant_id = ?
		`, option.userID, option.ID).Scan(&passwordHash, &role)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read tenant account"})
			return
		}
		if !utils.CheckPassword(password, passwordHash) {
			continue
		}

		option.Role = normalizeRole(role)
		stores = append(stores, option)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read account list"})
		return
	}

	if len(stores) == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"message": "Authentication failed",
			"errors":  []string{"Email or password is incorrect"},
		})
		return
	}

	if tenantID == "" && len(stores) > 1 {
		c.JSON(http.StatusOK, gin.H{
			"message": "Choose a store",
			"stores":  stores,
		})
		return
	}

	selected := stores[0]
	var name string
	err = getStoreDB(selected.ID, func(tenantDB *sql.DB) error {
		return tenantDB.QueryRow(
			"SELECT name FROM users WHERE id = ?",
			selected.userID,
		).Scan(&name)
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account"})
		return
	}

	token, err := utils.GenerateJWT(selected.userID, selected.ID, selected.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token generation failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Login successful",
		"name":      name,
		"role":      selected.Role,
		"token":     token,
		"tenant_id": selected.ID,
		"tenant": gin.H{
			"id":   selected.ID,
			"name": selected.Name,
		},
		"stores": stores,
	})
}

func normalizeRole(role string) string {
	if role == "admin" {
		return "owner"
	}
	return role
}

func getStoreDB(tenantID string, fn func(*sql.DB) error) error {
	var dbName string
	if err := db.Master.QueryRow(`
		SELECT db_name FROM tenants
		WHERE id = ? AND status = 'active'
	`, tenantID).Scan(&dbName); err != nil {
		return err
	}

	tenantDB, err := db.GetTenantDB(dbName)
	if err != nil {
		return err
	}
	return fn(tenantDB)
}
