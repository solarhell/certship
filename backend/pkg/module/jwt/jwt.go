package jwt

import (
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type claims struct {
	AdminID string `json:"admin_id"`
	jwtlib.RegisteredClaims
}

func GenerateToken(adminID, secret string) (string, error) {
	t := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims{
		AdminID:  adminID,
		ID:       uuid.NewString(),
		IssuedAt: jwtlib.NewNumericDate(time.Now()),
	})
	return t.SignedString([]byte(secret))
}

func ParseToken(token, secret string) (adminID string, err error) {
	t, err := jwtlib.ParseWithClaims(token, &claims{}, func(t *jwtlib.Token) (any, error) {
		if _, ok := t.Method.(*jwtlib.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return "", err
	}
	c, ok := t.Claims.(*claims)
	if !ok || !t.Valid {
		return "", fmt.Errorf("invalid token")
	}
	return c.AdminID, nil
}
