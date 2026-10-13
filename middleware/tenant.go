package middleware

import (
	"database/sql"
	"net/http"

	"assistant/db"

	"github.com/gin-gonic/gin"
)

func TenantMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {

		tenantID := c.GetHeader("X-Tenant-ID")
		tokenTenantID := c.GetString("tenant_id")
		if tenantID == "" {
			tenantID = tokenTenantID
		}

		if tenantID == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Tenant ID required"})
			return
		}
		userID := c.GetString("user_id")
		if userID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return
		}
		if tokenTenantID != "" && tenantID != tokenTenantID {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Tenant does not match authenticated account"})
			return
		}

		var dbName string
		err := db.Master.QueryRow(`
			SELECT t.db_name
			FROM tenants t
			JOIN users_index ui ON ui.tenant_id = t.id
			WHERE t.id = ? AND t.status = 'active' AND ui.user_id = ?
		`, tenantID, userID).Scan(&dbName)

		if err != nil {
			if err == sql.ErrNoRows {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Store membership is no longer active"})
			} else {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify store membership"})
			}
			return
		}

		conn, err := db.GetTenantDB(dbName)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "DB connection failed"})
			return
		}
		var memberID string
		if err := conn.QueryRow(
			"SELECT id FROM users WHERE id = ? AND tenant_id = ?",
			userID,
			tenantID,
		).Scan(&memberID); err != nil {
			if err == sql.ErrNoRows {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Store membership is no longer active"})
			} else {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify tenant account"})
			}
			return
		}

		c.Set("tenant_id", tenantID)
		c.Set("tenant_db", conn)
		c.Set("tenant_db_name", dbName)

		c.Next()
	}
}
