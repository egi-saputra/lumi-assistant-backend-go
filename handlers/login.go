package handlers

import (
    "cbt-api/db"
    "cbt-api/models"
    "cbt-api/utils"
    "fmt"
    "net/http"

    "github.com/gin-gonic/gin"
)

func Login(c *gin.Context) {
    var req struct {
        Email    string `json:"email"`
        Password string `json:"password"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
        return
    }

    email := req.Email
    password := req.Password
    fmt.Println("Login attempt:", email)

    var user models.User
    err := db.DB.QueryRow(
        "SELECT id, name, email, password, role FROM users WHERE email=?",
        email,
    ).Scan(&user.ID, &user.Name, &user.Email, &user.Password, &user.Role)

    if err != nil {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
        return
    }

    if !utils.CheckPassword(password, user.Password) {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "Wrong password"})
        return
    }

    token, err := utils.GenerateJWT(user.ID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "message": "Login successful",
        "role":    user.Role,
        "token":   token,
    })
}
