package auth

import (
	"database/sql"
	"assistant/db"
	"assistant/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func RegisterDeveloper(c *gin.Context) {

	var req struct {
		Name     string `json:"name" binding:"required"`
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required,min=6"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	// =========================
	// 1. cek email unik
	// =========================
	var exists string

	err := db.Master.QueryRow(`
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
	// 3. cek apakah user pertama
	// =========================
	var count int
	err = db.Master.QueryRow(`
		SELECT COUNT(*) FROM users
	`).Scan(&count)

	if err != nil {
		c.JSON(500, gin.H{"error": "Database error"})
		return
	}

	role := "developer"
	if count == 0 {
		role = "admin"
	}

	// =========================
	// 4. hash password
	// =========================
	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		c.JSON(500, gin.H{"error": "Password hash failed"})
		return
	}

	userID := utils.NewULID()

	// =========================
	// 5. insert
	// =========================
	_, err = db.Master.Exec(`
		INSERT INTO users (id, name, email, password_hash, role)
		VALUES (?, ?, ?, ?, ?)
	`, userID, req.Name, req.Email, hash, role)

	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// =========================
	// RESPONSE
	// =========================
	c.JSON(http.StatusCreated, gin.H{
		"message": "Developer registered",
		"role":    role,
	})
}
