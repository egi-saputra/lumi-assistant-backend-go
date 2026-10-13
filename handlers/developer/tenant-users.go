package dev

import (
	"assistant/db"
	"net/http"

	"github.com/gin-gonic/gin"
)

type TenantUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	Role  string `json:"role"`
}

func GetTenantUsers(c *gin.Context) {

	tenantID := c.Param("id")

	// ambil db_name
	var dbName string

	err := db.Master.QueryRow(`
		SELECT db_name
		FROM tenants
		WHERE id = ?
	`, tenantID).Scan(&dbName)

	if err != nil {
		c.JSON(404, gin.H{
			"error": "tenant not found",
		})
		return
	}

	tenantDB, err := db.GetTenantDB(dbName)

	if err != nil {
		c.JSON(500, gin.H{
			"error": "db failed",
		})
		return
	}

	rows, err := tenantDB.Query(`
		SELECT id, name, email, phone, role
		FROM users
		ORDER BY created_at DESC
	`)

	if err != nil {
		c.JSON(500, gin.H{
			"error": "query failed",
		})
		return
	}

	defer rows.Close()

	var users []TenantUser

	for rows.Next() {

		var u TenantUser

		err := rows.Scan(
			&u.ID,
			&u.Name,
			&u.Email,
			&u.Phone,
			&u.Role,
		)

		if err != nil {
			continue
		}

		users = append(users, u)
	}

	c.JSON(http.StatusOK, users)
}