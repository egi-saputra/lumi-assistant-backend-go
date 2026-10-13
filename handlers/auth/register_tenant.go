package auth

import (
	"errors"
	"net/http"
	"strings"

	"assistant/db"
	"assistant/utils"

	"github.com/gin-gonic/gin"
	mysql "github.com/go-sql-driver/mysql"
)

type RegisterTenantRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

func RegisterTenant(c *gin.Context) {
	var req RegisterTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Password hash failed"})
		return
	}

	userID := utils.NewULID()
	_, err = db.Master.Exec(`
		INSERT INTO accounts (id, name, email, password_hash)
		VALUES (?, ?, ?, ?)
	`, userID, req.Name, req.Email, hash)
	if err != nil {
		if isDuplicateKeyError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Account creation failed"})
		return
	}

	token, err := utils.GenerateJWT(userID, "", "account")
	if err != nil {
		_, deleteErr := db.Master.Exec("DELETE FROM accounts WHERE id = ?", userID)
		if deleteErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Token generation failed and account cleanup failed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Token generation failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":   "Account created",
		"tenant_id": "",
		"name":      req.Name,
		"role":      "account",
		"token":     token,
	})
}

func isDuplicateKeyError(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
