package dev

import (
	"assistant/db"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type User struct {
	UserID string `json:"user_id"`
	Name  string `json:"name"`
	Email  string `json:"email"`
	Phone  string `json:"phone"`
	Role   string `json:"role"`
}

func GetDeveloperDashboard(c *gin.Context) {

	role := c.GetString("role")
	userID := c.GetString("user_id")

	if role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "forbidden",
		})
		return
	}

	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "unauthorized",
		})
		return
	}

	// =========================
	// AMBIL USER DARI MASTER_DB USERS
	// =========================
	var user User

	err := db.Master.QueryRow(`
		SELECT id, name, email, phone, role
		FROM users
		WHERE id = ?
		LIMIT 1
	`, userID).Scan(
		&user.UserID,
		&user.Name,
		&user.Email,
		&user.Phone,
		&user.Role,
	)

if err != nil {
	log.Printf("DEV_DASH ERROR: %v", err)
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": err.Error(),
	})
	return
}

	// =========================
	// RESPONSE
	// =========================
	c.JSON(http.StatusOK, gin.H{
		"user": user,
	})
}