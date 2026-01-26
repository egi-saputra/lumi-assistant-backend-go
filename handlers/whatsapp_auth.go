package handlers

import (
	// "bytes"
	"cbt-api/db"
	"cbt-api/utils"
	// "encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

/* =========================
   REQUEST OTP
========================= */
func RequestOtp(c *gin.Context) {
	var req struct {
		Phone string `json:"phone"`
	}

	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Phone) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Phone is required"})
		return
	}

	phone := utils.NormalizePhone(req.Phone)

	otp := fmt.Sprintf("%06d", rand.Intn(1000000))
	expiresAt := time.Now().Add(5 * time.Minute)

	fmt.Println("===== RequestOtp DEBUG =====")
	fmt.Println("Original phone:", req.Phone)
	fmt.Println("Normalized phone:", phone)
	fmt.Println("Generated OTP:", otp)
	fmt.Println("ExpiresAt:", expiresAt.Format("2006-01-02 15:04:05"))

	_, err := db.DB.Exec(`
		INSERT INTO whatsapp_otps (phone, otp, expires_at)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE otp = ?, expires_at = ?
	`,
		phone,
		otp,
		expiresAt.Format("2006-01-02 15:04:05"),
		otp,
		expiresAt.Format("2006-01-02 15:04:05"),
	)

	if err != nil {
		fmt.Println("DB insert error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save OTP"})
		return
	}

	go utils.SendWhatsapp(phone, fmt.Sprintf(
		"🔐 Kode OTP kamu: %s\n⏱ Berlaku 5 menit",
		otp,
	))

	c.JSON(http.StatusOK, gin.H{"message": "OTP sent successfully"})
}

/* =========================
   VERIFY OTP
========================= */
func VerifyOtp(c *gin.Context) {
	var req struct {
		Phone string `json:"phone"`
		OTP   string `json:"otp"`
	}

	if err := c.ShouldBindJSON(&req); err != nil ||
		strings.TrimSpace(req.Phone) == "" ||
		strings.TrimSpace(req.OTP) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Phone and OTP required"})
		return
	}

	phone := utils.NormalizePhone(req.Phone)
	otpInput := strings.TrimSpace(req.OTP)

	fmt.Println("===== VerifyOtp DEBUG =====")
	fmt.Println("Original phone:", req.Phone)
	fmt.Println("Normalized phone:", phone)
	fmt.Println("OTP input:", otpInput)

	var dbOtp string
	var expiresAtStr string

	err := db.DB.QueryRow(`
		SELECT otp, expires_at
		FROM whatsapp_otps
		WHERE phone = ?
		ORDER BY id DESC
		LIMIT 1
	`, phone).Scan(&dbOtp, &expiresAtStr)

	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "OTP invalid or expired"})
		return
	}

	expiresAt, err := time.Parse("2006-01-02 15:04:05", expiresAtStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid OTP expiry"})
		return
	}

	if otpInput != strings.TrimSpace(dbOtp) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "OTP salah"})
		return
	}

	if time.Now().After(expiresAt) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "OTP expired"})
		return
	}

	// hapus OTP
	_, _ = db.DB.Exec("DELETE FROM whatsapp_otps WHERE phone = ?", phone)

	// cari / buat user
	var userID int
	err = db.DB.QueryRow("SELECT id FROM users WHERE phone = ?", phone).Scan(&userID)

	if err != nil {
		randomPass := fmt.Sprintf("%d", rand.Int63())
		hash, _ := utils.HashPassword(randomPass)

		res, _ := db.DB.Exec(`
			INSERT INTO users (name, phone, password)
			VALUES (?, ?, ?)
		`,
			"User "+phone[len(phone)-4:],
			phone,
			hash,
		)

		lastID, _ := res.LastInsertId()
		userID = int(lastID)
	}

	token, _ := utils.GenerateJWT(userID)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":    userID,
			"phone": phone,
		},
	})
}
