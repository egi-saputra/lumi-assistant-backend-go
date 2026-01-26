package handlers

import (
	"cbt-api/db"
	"cbt-api/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func Register(c *gin.Context) {
	var req struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
	}

	// bind JSON
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	req.Phone = strings.TrimSpace(req.Phone)
	req.Password = strings.TrimSpace(req.Password)

	// basic validation
	if req.Name == "" || req.Email == "" || req.Phone == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "All fields are required"})
		return
	}

	if len(req.Password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password minimal 6 karakter"})
		return
	}

	// normalisasi phone (0 -> 62)
	phone := utils.NormalizePhone(req.Phone)

	// cek email sudah ada
	var existingID int
	err := db.DB.QueryRow(
		"SELECT id FROM users WHERE email = ?",
		req.Email,
	).Scan(&existingID)

	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Email already registered"})
		return
	}

	// cek phone sudah ada
	err = db.DB.QueryRow(
		"SELECT id FROM users WHERE phone = ?",
		phone,
	).Scan(&existingID)

	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Phone already registered"})
		return
	}

	// hash password
	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// insert user
	res, err := db.DB.Exec(`
		INSERT INTO users (name, email, phone, password, role)
		VALUES (?, ?, ?, ?, 'siswa')
	`, req.Name, req.Email, phone, hash)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	lastID, _ := res.LastInsertId()
	userID := int(lastID)

	// generate JWT
	token, err := utils.GenerateJWT(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Register successful",
		"token":   token,
		"user": gin.H{
			"id":    userID,
			"name":  req.Name,
			"email": req.Email,
			"phone": phone,
			"role":  "siswa",
		},
	})
}
