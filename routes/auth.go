package routes

import (
	authHandlers "assistant/handlers/auth"
	"assistant/middleware"

	"github.com/gin-gonic/gin"
)

func AuthRoutes(r *gin.Engine) {


	r.POST("/register_dev", authHandlers.RegisterDeveloper)
	r.POST("/register_tenant", authHandlers.RegisterTenant)

	r.POST("/login_dev", authHandlers.LoginDeveloper)
	r.POST("/login", authHandlers.Login)
	r.POST("/payments/midtrans/notification", authHandlers.MidtransNotification)

	auth := r.Group("/")
	auth.Use(middleware.AuthMiddleware())
	auth.GET("/me", authHandlers.Me)
	auth.PUT("/me", authHandlers.UpdateAccount)
	auth.GET("/subscription/payments", authHandlers.GetSubscriptionPaymentHistory)
	auth.GET("/stores", authHandlers.ListStores)
	auth.POST("/stores", authHandlers.CreateStore)
	auth.POST("/stores/join", authHandlers.JoinStore)
	auth.POST("/stores/switch", authHandlers.SwitchStore)
	auth.GET("/stores/members", middleware.RequireRole("owner", "cashier", "warehouse", "courier", "finance"), authHandlers.ListStoreMembers)
	auth.PUT("/stores/name", middleware.RequireRole("owner"), authHandlers.RenameStore)

	owner := auth.Group("/")
	owner.Use(middleware.RequireRole("owner"))
	owner.GET("/subscription", authHandlers.GetSubscription)
	owner.POST("/subscription/checkout", authHandlers.CreateSubscriptionCheckout)
	owner.POST("/stores/members", authHandlers.AddStoreMember)
	owner.PUT("/stores/members/:userID", authHandlers.UpdateStoreMember)
	owner.DELETE("/stores/members/:userID", authHandlers.RemoveStoreMember)
}
