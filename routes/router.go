package routes

import (
	"assistant/handlers"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func Setup() *gin.Engine {
	router := gin.Default()
	fmt.Println("🚀 SETUP ROUTES ACTIVE")

	allowedOrigins := []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
		"http://localhost:19006",
		"https://assistant.lumiverse.co.id",
	}
	for _, origin := range strings.Split(os.Getenv("STOREFRONT_ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowedOrigins = append(allowedOrigins, origin)
		}
	}

	router.Use(cors.New(cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Tenant-ID"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	AuthRoutes(router)
	DevRoutes(router)
	AuthTenantRoutes(router)
	router.GET("/public/stores/:slug/products", handlers.PublicStoreProducts)
	router.GET("/public/stores/:slug/logo", handlers.PublicStoreLogo)
	router.GET("/public/stores/:slug/product-images/:productID", handlers.PublicStoreProductImage)

	return router
}
