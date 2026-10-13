package dev

import (
	"assistant/db"
	"assistant/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetUsers(c *gin.Context) {

	tenantID := c.GetString("tenant_id")

	if tenantID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "invalid tenant",
		})
		return
	}

	// ambil db_name
	var dbName string
	err := db.Master.QueryRow(`
		SELECT db_name FROM tenants WHERE id = ?
	`, tenantID).Scan(&dbName)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "tenant not found",
		})
		return
	}

	// connect tenant DB
	conn, err := db.GetTenantDB(dbName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "db connection failed",
		})
		return
	}

	rows, err := conn.Query(`
		SELECT id, name, email, created_at
		FROM users
		ORDER BY created_at DESC
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "query failed",
		})
		return
	}
	defer rows.Close()

	var users []models.User

	for rows.Next() {
		var u models.User

		err := rows.Scan(
			&u.ID,
			&u.Name,
			&u.Email,
			&u.CreatedAt,
		)

		if err != nil {
			continue
		}

		users = append(users, u)
	}

	c.JSON(http.StatusOK, gin.H{
		"data": users,
	})
}