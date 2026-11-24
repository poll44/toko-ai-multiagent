// Package auth: penerbitan & verifikasi JWT HS256 (golang-jwt/jwt/v5) — TRD 01 §5.
package auth // nama package: auth

import ( // package yang dipakai
	"fmt"  // format error
	"time" // durasi & waktu

	"github.com/golang-jwt/jwt/v5" // library JWT
)

// Claims: isi (payload) token JWT.
type Claims struct {
	Email                string `json:"email"` // email user
	Name                 string `json:"name"`  // nama user
	Role                 string `json:"role"`  // role user
	jwt.RegisteredClaims        // klaim standar (sub, iat, exp)
}

// Sign menerbitkan token HS256; Subject = user id.
func Sign(secret string, ttl time.Duration, userID, email, name, role string) (string, error) {
	now := time.Now() // waktu penerbitan
	c := Claims{
		Email: email, Name: name, Role: role, // klaim kustom
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,                           // sub = ID user
			IssuedAt:  jwt.NewNumericDate(now),          // iat = waktu terbit
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)), // exp = waktu kedaluwarsa
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret)) // tanda tangani dengan secret
}

// Parse memverifikasi token. Metode DIBATASI HS256 (anti alg=none / konfusi algoritma) dan exp wajib.
func Parse(secret, token string) (*Claims, error) {
	c := &Claims{} // penampung klaim
	t, err := jwt.ParseWithClaims(token, c,
		func(*jwt.Token) (any, error) { return []byte(secret), nil }, // kunci verifikasi
		jwt.WithValidMethods([]string{"HS256"}),                      // hanya HS256
		jwt.WithExpirationRequired(),                                 // exp wajib ada
	)
	if err != nil { // tanda tangan salah / kedaluwarsa / format rusak
		return nil, err
	}
	if !t.Valid || c.Subject == "" { // token tidak valid atau tanpa subject
		return nil, fmt.Errorf("token tidak valid")
	}
	return c, nil
}
