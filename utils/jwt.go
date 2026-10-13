package utils

import (
	"errors"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var jwtKey = loadJWTKey()

// loadJWTKey membaca secret dari environment.
// Di production (APP_ENV=production) JWT_SECRET wajib diisi minimal 32 karakter.
func loadJWTKey() []byte {
	if secret := os.Getenv("JWT_SECRET"); len(secret) >= 32 {
		return []byte(secret)
	}
	if os.Getenv("APP_ENV") == "production" {
		log.Fatal("JWT_SECRET must be set (minimum 32 characters) when APP_ENV=production")
	}
	log.Println("WARNING: JWT_SECRET not set; using insecure development key")
	return []byte("secret_key")
}

// =========================
// GENERATE JWT (FINAL)
// =========================
func GenerateJWT(userID string, tenantID string, role string) (string, error) {
	claims := jwt.MapClaims{
		"user_id":   userID,
		"tenant_id": tenantID,
		"role":      role,
		"exp":       time.Now().Add(24 * time.Hour).Unix(),
		"iat":       time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtKey)
}

// =========================
// PARSE JWT (FINAL)
// =========================
type AuthClaims struct {
	UserID   string
	TenantID string
	Role     string
}

func ParseJWT(tokenStr string) (*AuthClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtKey, nil
	}, jwt.WithValidMethods([]string{"HS256"}))

	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}

	uid, ok1 := claims["user_id"].(string)
	tid, ok2 := claims["tenant_id"].(string)
	role, ok3 := claims["role"].(string)

	if !ok1 || !ok2 || !ok3 {
		return nil, errors.New("missing jwt claims")
	}

	return &AuthClaims{
		UserID:   uid,
		TenantID: tid,
		Role:     role,
	}, nil
}