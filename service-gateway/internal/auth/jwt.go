// Package auth: penerbitan & verifikasi JWT HS256 (golang-jwt/jwt/v5) — PRD 01 §5.
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

// Sign menerbitkan token HS256; Subject = user id.
func Sign(secret string, ttl time.Duration, userID, email, name, role string) (string, error) {
	now := time.Now()
	c := Claims{
		Email: email, Name: name, Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
}

// Parse memverifikasi token. Metode DIBATASI HS256 (anti alg=none / konfusi algoritma) dan exp wajib.
func Parse(secret, token string) (*Claims, error) {
	c := &Claims{}
	t, err := jwt.ParseWithClaims(token, c,
		func(*jwt.Token) (any, error) { return []byte(secret), nil },
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	if !t.Valid || c.Subject == "" {
		return nil, fmt.Errorf("token tidak valid")
	}
	return c, nil
}
