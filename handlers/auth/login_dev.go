package auth

import (
	"assistant/db"
	"assistant/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func LoginDeveloper(c *gin.Context) {

	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	// =========================
	// 1. GET USER FROM MASTER
	// =========================
	var userID, hash, role, name string

	err := db.Master.QueryRow(`
		SELECT id, password_hash, role, name
		FROM users
		WHERE email = ?
		AND role = 'admin'
	`, req.Email).Scan(&userID, &hash, &role, &name)

	if err != nil {
		// c.JSON(401, gin.H{"error": "Developer not found"})
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Developer not found"})
		return
	}

	// =========================
	// 2. CHECK PASSWORD
	// =========================
	if !utils.CheckPassword(req.Password, hash) {
		// c.JSON(401, gin.H{"error": "Wrong password"})
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Wrong password"})
		return
	}

	// =========================
	// 3. JWT (NO TENANT)
	// =========================
	token, err := utils.GenerateJWT(userID, "", role)
	if err != nil {
		c.JSON(500, gin.H{"error": "Token failed"})
		return
	}

	// c.JSON(200, gin.H{
	// 	"message": "Developer login successful",
	// 	"name":    name,
	// 	"role":    role,
	// 	"token":   token,
	// })

	c.JSON(http.StatusOK, gin.H{
		"message": "Developer login successful",
		"name":    name,
		"role":    role,
		"token":   token,
	})
}
