package middleware

import (
	"assistant/utils"
	"strings"

	"github.com/gin-gonic/gin"
)

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {

		auth := c.GetHeader("Authorization")
		if auth == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "Unauthorized"})
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")

		claims, err := utils.ParseJWT(token)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "Invalid token"})
			return
		}

		// =========================
		// inject context (NEW STRUCT)
		// =========================
		c.Set("user_id", claims.UserID)
		c.Set("tenant_id", claims.TenantID)
		role := claims.Role
		if role == "admin" {
			role = "owner"
		}
		c.Set("role", role)

		c.Next()
	}
}
