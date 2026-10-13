package routes

import (
	"assistant/handlers"
	devHandlers "assistant/handlers/developer"
	"assistant/middleware"

	"github.com/gin-gonic/gin"
)

func DevRoutes(r *gin.Engine) {

	auth := r.Group("/")
	auth.Use(middleware.AuthMiddleware())
	auth.Use(middleware.RequireRole("admin"))

	auth.GET("/tenants", handlers.GetTenants)
	auth.GET("/dev_dash", devHandlers.GetDeveloperDashboard)
	auth.GET("/developer", devHandlers.GetDeveloperUsers)
	auth.GET("/users-master", devHandlers.GetUsers)
	auth.GET("/tenant-users/:id", devHandlers.GetTenantUsers)
}
