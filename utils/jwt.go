package utils

import (
    "errors"
    "time"

    "github.com/golang-jwt/jwt/v5"
)

var jwtKey = []byte("secret_key") // ganti dengan secret aman

func GenerateJWT(userID int) (string, error) {
    claims := jwt.MapClaims{
        "user_id": userID,
        "exp":     time.Now().Add(24 * time.Hour).Unix(),
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(jwtKey)
}

func ParseJWT(tokenStr string) (int, error) {
    token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
        if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, errors.New("unexpected signing method")
        }
        return jwtKey, nil
    })

    if err != nil || !token.Valid {
        return 0, errors.New("invalid token")
    }

    claims, ok := token.Claims.(jwt.MapClaims)
    if !ok {
        return 0, errors.New("invalid claims")
    }

    userIDFloat, ok := claims["user_id"].(float64)
    if !ok {
        return 0, errors.New("invalid user_id in token")
    }

    return int(userIDFloat), nil
}
