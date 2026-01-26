package main

import (
    "cbt-api/db"
    "cbt-api/handlers"
	"cbt-api/middleware"

	"time"
    "github.com/gin-gonic/gin"
    "github.com/gin-contrib/cors"
)

func main() {
    db.Connect()

    router := gin.Default()

	// --- CORS middleware ---
    router.Use(cors.New(cors.Config{
        AllowOrigins:     []string{"*"}, // untuk development, bisa diganti domain frontend
		// AllowOrigins: []string{"http://localhost:19006"}, // Expo default
        AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
        AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
        ExposeHeaders:    []string{"Content-Length"},
        AllowCredentials: true,
        MaxAge: 12 * time.Hour,
    }))
    // --- end CORS middleware ---

    router.POST("/login", handlers.Login)
    router.POST("/register", handlers.Register)

	router.POST("/whatsapp/request-otp", handlers.RequestOtp)
	router.POST("/whatsapp/verify-otp", handlers.VerifyOtp)

    // --- Protected routes ---
    auth := router.Group("/")
    auth.Use(middleware.AuthMiddleware())
    auth.GET("/user", handlers.GetUser)

	router.Run("0.0.0.0:8080")
	// router.Run(":8080") // server running di port 8080
}
