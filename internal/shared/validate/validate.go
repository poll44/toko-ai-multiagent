// Package validate: validator ringan tanpa framework. Mengumpulkan semua error lalu
// menghasilkan *httputil.APIError (VALIDATION_ERROR, details: [{field,message}]).
package validate

import (
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"toko-ai-microservices/internal/shared/httputil"
)

type V struct{ errs []httputil.FieldError }

func New() *V { return &V{} }

func (v *V) Add(field, msg string) {
	v.errs = append(v.errs, httputil.FieldError{Field: field, Message: msg})
}

// Required: string tidak kosong (setelah trim). Mengembalikan true bila terisi.
func (v *V) Required(field, s string) bool {
	if strings.TrimSpace(s) == "" {
		v.Add(field, "wajib diisi")
		return false
	}
	return true
}

// Len: panjang karakter (rune) setelah trim harus min..max.
func (v *V) Len(field, s string, min, max int) {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	if n < min || n > max {
		v.Add(field, fmt.Sprintf("panjang harus %d–%d karakter", min, max))
	}
}

func (v *V) Int(field string, n, min, max int) {
	if n < min || n > max {
		v.Add(field, fmt.Sprintf("harus bernilai %d–%d", min, max))
	}
}

func (v *V) OneOf(field, s string, allowed ...string) {
	if !slices.Contains(allowed, s) {
		v.Add(field, "harus salah satu dari: "+strings.Join(allowed, ", "))
	}
}

func (v *V) Regex(field, s string, re *regexp.Regexp, msg string) {
	if !re.MatchString(s) {
		v.Add(field, msg)
	}
}

func (v *V) Email(field, s string) {
	if a, err := mail.ParseAddress(s); err != nil || a.Address != s {
		v.Add(field, "format email tidak valid")
	}
}

// Err mengembalikan nil bila valid. Gunakan: if e := v.Err(); e != nil { return e }
// (jangan menugaskan hasilnya langsung ke variabel bertipe error — jebakan nil interface).
func (v *V) Err() *httputil.APIError {
	if len(v.errs) == 0 {
		return nil
	}
	return httputil.ErrValidation("Data yang dikirim tidak valid.", v.errs)
}
