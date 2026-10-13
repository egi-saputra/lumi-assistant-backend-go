package utils

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var jwtKey = []byte("secret_key") // nanti pindah ke env

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
	})

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