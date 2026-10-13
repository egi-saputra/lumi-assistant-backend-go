package dev

import (
	"assistant/db"
	"database/sql"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type DevDataResponse struct {
	TenantID   string
	TenantName string
	Code       string
	DBName     string
	Plan       string
	Status     string

	MaxUsers sql.NullInt64

	SubscriptionEnd sql.NullString
	TenantCreatedAt string

	UserID sql.NullString
	Email  sql.NullString
	Phone  sql.NullString

	UserCreatedAt string
}

type DevDataJSON struct {
	TenantID        string  `json:"tenant_id"`
	TenantName      string  `json:"tenant_name"`
	Code            string  `json:"code"`
	DBName          string  `json:"db_name"`
	Plan            string  `json:"plan"`
	Status          string  `json:"status"`
	MaxUsers        int     `json:"max_users"`
	SubscriptionEnd *string `json:"subscription_end"`
	TenantCreatedAt string  `json:"tenant_created_at"`

	UserID        *string `json:"user_id"`
	Email         *string `json:"email"`
	Phone         *string `json:"phone"`
	UserCreatedAt *string `json:"user_created_at"`
}

func GetDeveloperUsers(c *gin.Context) {

	rows, err := db.Master.Query(`
		SELECT 
			t.id,
			t.name,
			t.code,
			t.db_name,
			t.plan,
			t.status,
			t.max_users,
			t.subscription_end,
			t.created_at,

			ui.user_id,
			ui.email,
			ui.phone,
			ui.created_at

		FROM tenants t
		LEFT JOIN users_index ui ON ui.tenant_id = t.id
		ORDER BY t.created_at DESC
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}
	defer rows.Close()

	var result []DevDataJSON

	for rows.Next() {

		var u DevDataResponse

		err := rows.Scan(
			&u.TenantID,
			&u.TenantName,
			&u.Code,
			&u.DBName,
			&u.Plan,
			&u.Status,
			&u.MaxUsers,
			&u.SubscriptionEnd,
			&u.TenantCreatedAt,

			&u.UserID,
			&u.Email,
			&u.Phone,
			&u.UserCreatedAt,
		)

		if err != nil {
			log.Printf("SCAN ERROR: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		}

		// ===== SAFE FORMAT =====
		var subEnd *string
		if u.SubscriptionEnd.Valid {
			s := u.SubscriptionEnd.String
			subEnd = &s
		}

		var tenantCreated string
		if u.TenantCreatedAt != "" {
			tenantCreated = u.TenantCreatedAt
		} else {
			tenantCreated = "-"
		}

		var userCreated *string
		if u.UserCreatedAt != "" {
			userCreated = &u.UserCreatedAt
		}

		// ===== SAFE POINTERS =====
		var userID, email, phone *string

		if u.UserID.Valid {
			userID = &u.UserID.String
		}
		if u.Email.Valid {
			email = &u.Email.String
		}
		if u.Phone.Valid {
			phone = &u.Phone.String
		}

		result = append(result, DevDataJSON{
			TenantID:        u.TenantID,
			TenantName:      u.TenantName,
			Code:            u.Code,
			DBName:          u.DBName,
			Plan:            u.Plan,
			Status:          u.Status,
			MaxUsers: func() int {
				if u.MaxUsers.Valid {
					return int(u.MaxUsers.Int64)
				}
				return 0
			}(),
			SubscriptionEnd: subEnd,
			TenantCreatedAt: tenantCreated,

			UserID:        userID,
			Email:         email,
			Phone:         phone,
			UserCreatedAt: userCreated,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data": result,
	})
}