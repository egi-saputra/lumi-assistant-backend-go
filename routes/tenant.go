package routes

import (
	"fmt"

	"assistant/handlers"
	// authHandlers "assistant/handlers/authTenant"
	"assistant/middleware"

	"github.com/gin-gonic/gin"
)

func AuthTenantRoutes(r *gin.Engine) {
	fmt.Println("REGISTER /users-tenant ROUTE")

	auth := r.Group("/")
	auth.Use(middleware.AuthMiddleware())
	auth.Use(middleware.TenantMiddleware())

	auth.GET("/invoice-quota", middleware.RequireRole("owner", "cashier"), handlers.GetInvoiceQuota)
	auth.POST("/invoice-quota/reward-attempt", middleware.RequireRole("owner", "cashier"), handlers.StartInvoiceAdReward)
	auth.POST("/invoice-quota/reward-attempt/cancel", middleware.RequireRole("owner", "cashier"), handlers.CancelInvoiceAdReward)
	auth.POST("/invoice-quota/fallback/start", middleware.RequireRole("owner", "cashier"), handlers.StartInvoiceFallback)
	auth.POST("/invoice-quota/fallback/heartbeat", middleware.RequireRole("owner", "cashier"), handlers.HeartbeatInvoiceFallback)
	auth.POST("/invoice-quota/fallback/complete", middleware.RequireRole("owner", "cashier"), handlers.CompleteInvoiceFallback)
	auth.POST("/invoice-quota/fallback/cancel", middleware.RequireRole("owner", "cashier"), handlers.CancelInvoiceFallback)
	r.GET("/ads/rewarded/ssv", handlers.AdMobInvoiceRewardSSV)

	auth.GET("/users-tenant/count", handlers.GetTotalUsersTenant)
	auth.GET("/users-tenant", handlers.GetUserOnTenant)

	contacts := auth.Group("/contacts")
	contacts.Use(middleware.RequireRole("owner", "cashier", "warehouse", "courier", "finance"))
	contacts.GET("", handlers.ListContacts)
	contacts.POST("", handlers.CreateContact)
	contacts.PUT("/:id", handlers.UpdateContact)
	contacts.DELETE("/:id", handlers.DeleteContact)

	finance := auth.Group("/")
	finance.Use(middleware.RequireRole("owner", "cashier", "warehouse", "courier", "finance"))
	finance.GET("/expenses", handlers.ListExpenses)
	finance.POST("/expenses", handlers.CreateExpense)
	finance.GET("/cash-flows", handlers.ListCashFlows)
	finance.GET("/dashboard/summary", handlers.GetDashboardSummary)
	auth.GET(
		"/reports/summary",
		middleware.RequireRole("owner", "cashier", "finance"),
		handlers.GetReportSummary,
	)
	auth.GET(
		"/payments",
		middleware.RequireRole("owner", "cashier", "finance"),
		handlers.ListPaymentHistory,
	)

	products := auth.Group("/products")
	products.Use(middleware.RequireRole("owner", "cashier", "warehouse", "courier", "finance"))
	products.GET("/quota", handlers.GetProductQuota)
	products.GET("", handlers.ListProducts)
	products.GET("/:id", handlers.GetProduct)
	products.GET("/:id/image", handlers.GetProductImage)
	products.GET("/:id/stock-movements", handlers.ListStockMovements)
	auth.GET(
		"/stock-movements",
		middleware.RequireRole("owner", "cashier", "warehouse", "courier", "finance"),
		handlers.ListStockMovements,
	)
	auth.GET("/business-profile", middleware.RequireRole("owner", "cashier", "warehouse", "courier", "finance"), handlers.GetBusinessProfile)
	auth.PUT("/business-profile", middleware.RequireRole("owner"), handlers.UpdateBusinessProfile)
	auth.PUT("/business-profile/logo", middleware.RequireRole("owner"), handlers.UploadBusinessLogo)
	auth.GET("/social-links", middleware.RequireRole("owner", "cashier", "warehouse", "courier", "finance"), handlers.ListSocialLinks)
	auth.PUT("/social-links", middleware.RequireRole("owner"), handlers.SaveSocialLink)
	auth.DELETE("/social-links/:id", middleware.RequireRole("owner"), handlers.DeleteSocialLink)
	auth.GET("/payment-methods", middleware.RequireRole("owner", "cashier"), handlers.ListPaymentMethods)
	auth.POST("/payment-methods", middleware.RequireRole("owner"), handlers.CreatePaymentMethod)
	auth.DELETE("/payment-methods/:id", middleware.RequireRole("owner"), handlers.DeletePaymentMethod)
	auth.GET(
		"/notifications/snapshot",
		middleware.RequireRole("owner", "cashier", "warehouse", "courier", "finance"),
		handlers.GetNotificationSnapshot,
	)

	productManagers := auth.Group("/products")
	productManagers.Use(middleware.RequireRole("owner", "cashier", "warehouse"))
	productManagers.POST("/reward-attempt", handlers.StartProductReward)
	productManagers.POST("/reward-attempt/cancel", handlers.CancelProductReward)
	productManagers.POST("", handlers.CreateProduct)
	productManagers.PUT("/:id", handlers.UpdateProduct)
	productManagers.PUT("/:id/image", handlers.UploadProductImage)
	productManagers.DELETE("/:id", handlers.DeleteProduct)
	productManagers.POST("/:id/stock-movements", handlers.CreateStockMovement)

	sales := auth.Group("/")
	sales.Use(middleware.RequireRole("owner", "cashier"))
	sales.POST("/orders", handlers.CreateOrder)
	sales.GET("/orders", handlers.ListOrders)
	sales.GET("/orders/:id", handlers.GetOrder)
	sales.POST("/orders/:id/payments", handlers.CreateOrderPayment)
	sales.POST("/orders/:id/return", handlers.ReturnOrder)
	sales.POST("/orders/:id/refund", handlers.RefundOrderPayments)
	sales.DELETE("/orders/:id", handlers.DeleteOrder)

}
