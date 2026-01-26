package handlers

import (
    "cbt-api/db"
    "cbt-api/models"
    "net/http"
    "github.com/gin-gonic/gin"
)

func GetUser(c *gin.Context) {
    userID, ok := c.Get("userID")
    if !ok {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
        return
    }

    var user models.User
    err := db.DB.QueryRow(
        "SELECT id, name, email, role FROM users WHERE id=?",
        userID,
    ).Scan(&user.ID, &user.Name, &user.Email, &user.Role)

    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "id":    user.ID,
        "name":  user.Name,
        "email": user.Email,
        "role":  user.Role,
    })
}
