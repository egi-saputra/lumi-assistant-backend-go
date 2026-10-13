package auth

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"assistant/db"
	"assistant/utils"

	"github.com/gin-gonic/gin"
)

type accountUpdateRequest struct {
	Name            string `json:"name" binding:"required,max=100"`
	Email           string `json:"email" binding:"required,email,max=100"`
	Phone           string `json:"phone" binding:"max=20"`
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password"`
}

type accountStoreReplica struct {
	dbName       string
	tx           *sql.Tx
	name         string
	email        string
	phone        string
	passwordHash string
	committed    bool
}

func UpdateAccount(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req accountUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nama dan email wajib valid; nomor telepon maksimal 20 karakter."})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Phone = strings.TrimSpace(req.Phone)
	if req.Name == "" || !utf8.ValidString(req.Name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nama wajib diisi dengan teks yang valid."})
		return
	}
	if len(req.NewPassword) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password baru maksimal 72 byte."})
		return
	}
	if req.NewPassword != "" && len(req.NewPassword) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password baru minimal 8 karakter."})
		return
	}

	var currentHash string
	err := db.Master.QueryRow(`
		SELECT password_hash
		FROM accounts WHERE id = ?
	`, userID).Scan(&currentHash)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Account not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account"})
		return
	}
	if !utils.CheckPassword(req.CurrentPassword, currentHash) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Password saat ini tidak benar."})
		return
	}

	var emailOwner string
	err = db.Master.QueryRow("SELECT id FROM accounts WHERE email = ? AND id <> ?", req.Email, userID).Scan(&emailOwner)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Email sudah digunakan oleh akun lain."})
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate email"})
		return
	}

	newHash := currentHash
	if req.NewPassword != "" {
		newHash, err = utils.HashPassword(req.NewPassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
			return
		}
	}

	rows, err := db.Master.Query(`
		SELECT t.db_name
		FROM users_index ui
		JOIN tenants t ON t.id = ui.tenant_id
		WHERE ui.user_id = ?
	`, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load account stores"})
		return
	}
	dbNames := make([]string, 0)
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			rows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read account stores"})
			return
		}
		dbNames = append(dbNames, dbName)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read account stores"})
		return
	}
	if err := rows.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read account stores"})
		return
	}

	replicas := make([]accountStoreReplica, 0, len(dbNames))
	rollbackReplicas := func() {
		for _, replica := range replicas {
			if replica.tx != nil {
				if err := replica.tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
					log.Printf("failed to roll back account update in %s: %v", replica.dbName, err)
				}
			}
			if replica.committed {
				tenantDB, err := db.GetTenantDB(replica.dbName)
				if err != nil {
					log.Printf("failed to reconnect for account rollback in %s: %v", replica.dbName, err)
					continue
				}
				_, err = tenantDB.Exec(`
					UPDATE users SET name = ?, email = ?, phone = NULLIF(?, ''), password_hash = ?, updated_at = CURRENT_TIMESTAMP
					WHERE id = ?
				`, replica.name, replica.email, replica.phone, replica.passwordHash, userID)
				if err != nil {
					log.Printf("failed to restore account replica in %s: %v", replica.dbName, err)
				}
			}
		}
	}

	for _, dbName := range dbNames {
		tenantDB, err := db.GetTenantDB(dbName)
		if err != nil {
			rollbackReplicas()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to connect to an account store"})
			return
		}
		var conflictingUser string
		err = tenantDB.QueryRow("SELECT id FROM users WHERE email = ? AND id <> ?", req.Email, userID).Scan(&conflictingUser)
		if err == nil {
			rollbackReplicas()
			c.JSON(http.StatusConflict, gin.H{"error": "Email sudah digunakan di salah satu toko."})
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			rollbackReplicas()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate email in account stores"})
			return
		}
		var previous accountStoreReplica
		previous.dbName = dbName
		err = tenantDB.QueryRow(`
			SELECT name, email, COALESCE(phone, ''), password_hash
			FROM users WHERE id = ?
		`, userID).Scan(&previous.name, &previous.email, &previous.phone, &previous.passwordHash)
		if err != nil {
			rollbackReplicas()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Account membership is missing from a store"})
			return
		}
		tx, err := tenantDB.Begin()
		if err != nil {
			rollbackReplicas()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare account update"})
			return
		}
		previous.tx = tx
		replicas = append(replicas, previous)
		result, err := tx.Exec(`
			UPDATE users SET name = ?, email = ?, phone = NULLIF(?, ''), password_hash = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, req.Name, req.Email, req.Phone, newHash, userID)
		if err != nil {
			rollbackReplicas()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare account update in a store"})
			return
		}
		updated, err := result.RowsAffected()
		if err != nil || updated == 0 {
			rollbackReplicas()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Account membership is missing from a store"})
			return
		}
	}

	masterTx, err := db.Master.Begin()
	if err != nil {
		rollbackReplicas()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare account update"})
		return
	}
	if _, err := masterTx.Exec(`
		UPDATE accounts
		SET name = ?, email = ?, phone = NULLIF(?, ''), password_hash = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, req.Name, req.Email, req.Phone, newHash, userID); err != nil {
		_ = masterTx.Rollback()
		rollbackReplicas()
		c.JSON(http.StatusConflict, gin.H{"error": "Email sudah digunakan atau perubahan akun tidak dapat disimpan."})
		return
	}
	if _, err := masterTx.Exec(`
		UPDATE users_index SET email = ?, phone = NULLIF(?, '')
		WHERE user_id = ?
	`, req.Email, req.Phone, userID); err != nil {
		_ = masterTx.Rollback()
		rollbackReplicas()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update account store index"})
		return
	}

	for _, replica := range replicas {
		if err := replica.tx.Commit(); err != nil {
			_ = masterTx.Rollback()
			replica.tx = nil
			replica.committed = true
			rollbackReplicas()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Account update could not be synchronized to all stores. Please retry or contact support."})
			return
		}
		replica.tx = nil
		replica.committed = true
	}
	if err := masterTx.Commit(); err != nil {
		rollbackReplicas()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Account update could not be finalized. Please contact support."})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Account updated",
		"name":    req.Name,
		"email":   req.Email,
		"phone":   req.Phone,
	})
}
