package handlers

import (
	"database/sql"
	// "fmt"
	"assistant/db"
	"assistant/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetUserOnTenant(c *gin.Context) {

	userID := c.GetString("user_id")

	tenantDB, exists := c.Get("tenant_db")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "tenant db not found",
		})
		return
	}

	conn := tenantDB.(*sql.DB)

	// ambil tenant_id dari context (ini penting)
	tenantID := c.GetString("tenant_id")

	// ambil master DB
	masterDB := db.Master

	// query tenant name dari tabel tenants
	var tenantName string
	err := masterDB.QueryRow(`
		SELECT name
		FROM tenants
		WHERE id = ?
	`, tenantID).Scan(&tenantName)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "tenant not found",
		})
		return
	}

	// ambil user dari tenant DB
	var user models.User

	err = conn.QueryRow(`
		SELECT id, name, email, role
		FROM users
		WHERE id = ?
	`, userID).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.Role,
	)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "user not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user":        user,
		"tenant_name": tenantName,
	})
}

func GetTotalUsersTenant(c *gin.Context) {

	tenantDB, exists := c.Get("tenant_db")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "tenant db not found",
		})
		return
	}

	conn := tenantDB.(*sql.DB)

	var count int

	err := conn.QueryRow(`
		SELECT COUNT(*) 
		FROM users
	`).Scan(&count)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to count users",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total": count,
	})
}
