package handlers

import (
	"assistant/db"
	"net/http"

	"github.com/gin-gonic/gin"
)

type TenantResponse struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	DBName    string `json:"db_name"`
	Plan      string `json:"plan"`
	MaxUsers  int    `json:"max_users"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

func GetTenants(c *gin.Context) {

	rows, err := db.Master.Query(`
		SELECT
			id,
			code,
			name,
			db_name,
			plan,
			max_users,
			status,
			created_at
		FROM tenants
		ORDER BY created_at DESC
	`)

	if err != nil {
		c.JSON(500, gin.H{
			"error": "Failed to fetch tenants",
		})
		return
	}

	defer rows.Close()

	var tenants []TenantResponse

	for rows.Next() {

		var t TenantResponse

		err := rows.Scan(
			&t.ID,
			&t.Code,
			&t.Name,
			&t.DBName,
			&t.Plan,
			&t.MaxUsers,
			&t.Status,
			&t.CreatedAt,
		)

		if err != nil {
			continue
		}

		tenants = append(tenants, t)
	}

	c.JSON(http.StatusOK, tenants)
}
