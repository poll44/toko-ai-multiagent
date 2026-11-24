// Package validate: validator ringan tanpa framework. Mengumpulkan semua error lalu
// menghasilkan *httputil.APIError (VALIDATION_ERROR, details: [{field,message}]).
package validate // nama package: validate

import ( // package yang dipakai
	"fmt"          // format pesan
	"net/mail"     // parsing alamat email
	"regexp"       // regular expression
	"slices"       // slices.Contains
	"strings"      // utilitas string
	"unicode/utf8" // menghitung karakter (rune)

	"toko-ai-microservices/internal/shared/httputil" // FieldError & APIError
)

// V mengumpulkan semua kesalahan validasi.
type V struct{ errs []httputil.FieldError }

// New membuat validator kosong.
func New() *V { return &V{} }

// Add menambahkan satu kesalahan untuk field tertentu.
func (v *V) Add(field, msg string) {
	v.errs = append(v.errs, httputil.FieldError{Field: field, Message: msg}) // simpan error
}

// Required: string tidak kosong (setelah trim). Mengembalikan true bila terisi.
func (v *V) Required(field, s string) bool {
	if strings.TrimSpace(s) == "" { // kosong
		v.Add(field, "wajib diisi")
		return false
	}
	return true
}

// Len: panjang karakter (rune) setelah trim harus min..max.
func (v *V) Len(field, s string, min, max int) {
	n := utf8.RuneCountInString(strings.TrimSpace(s)) // hitung karakter
	if n < min || n > max {                           // di luar rentang
		v.Add(field, fmt.Sprintf("panjang harus %d–%d karakter", min, max))
	}
}

// Int: angka n harus berada pada rentang min..max.
func (v *V) Int(field string, n, min, max int) {
	if n < min || n > max { // di luar rentang
		v.Add(field, fmt.Sprintf("harus bernilai %d–%d", min, max))
	}
}

// OneOf: s harus salah satu dari nilai allowed.
func (v *V) OneOf(field, s string, allowed ...string) {
	if !slices.Contains(allowed, s) { // tidak termasuk daftar
		v.Add(field, "harus salah satu dari: "+strings.Join(allowed, ", "))
	}
}

// Regex: s harus cocok dengan pola re, jika tidak tambahkan pesan msg.
func (v *V) Regex(field, s string, re *regexp.Regexp, msg string) {
	if !re.MatchString(s) { // tidak cocok pola
		v.Add(field, msg)
	}
}

// Email: s harus berupa alamat email yang valid.
func (v *V) Email(field, s string) {
	if a, err := mail.ParseAddress(s); err != nil || a.Address != s { // gagal parse atau ada nama tampilan
		v.Add(field, "format email tidak valid")
	}
}

// Err mengembalikan nil bila valid. Gunakan: if e := v.Err(); e != nil { return e }
// (jangan menugaskan hasilnya langsung ke variabel bertipe error — jebakan nil interface).
func (v *V) Err() *httputil.APIError {
	if len(v.errs) == 0 { // tidak ada kesalahan
		return nil
	}
	return httputil.ErrValidation("Data yang dikirim tidak valid.", v.errs) // 400 dengan daftar field error
}
