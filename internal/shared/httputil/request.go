package httputil // helper HTTP bersama

import ( // package yang dipakai
	"encoding/json" // decode JSON
	"errors"        // pengecekan jenis error
	"io"            // konstanta io.EOF
	"mime"          // parsing Content-Type
	"net/http"      // tipe HTTP
	"strconv"       // konversi string ke angka/bool
)

// MaxBodyBytes: batas body request (1 MB).
const MaxBodyBytes = 1 << 20

// ReadJSON decode body JSON ke dst. Body kosong => VALIDATION_ERROR. Field tak dikenal diabaikan.
func ReadJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return readJSON(w, r, dst, false, false) // tidak strict, body kosong ditolak
}

// ReadJSONStrict seperti ReadJSON tetapi menolak field tak dikenal.
func ReadJSONStrict(w http.ResponseWriter, r *http.Request, dst any) error {
	return readJSON(w, r, dst, true, false) // strict, body kosong ditolak
}

// ReadJSONOptional seperti ReadJSON tetapi body kosong dianggap valid (dst tak diubah).
func ReadJSONOptional(w http.ResponseWriter, r *http.Request, dst any) error {
	return readJSON(w, r, dst, false, true) // tidak strict, body kosong diizinkan
}

// readJSON adalah implementasi inti pembacaan & validasi body JSON.
func readJSON(w http.ResponseWriter, r *http.Request, dst any, strict, allowEmpty bool) error {
	if r.ContentLength != 0 { // ada body
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")) // parse Content-Type
		if err != nil || mt != "application/json" {                     // bukan JSON
			return ErrValidation("Content-Type harus application/json.", nil)
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes) // batasi ukuran body
	dec := json.NewDecoder(r.Body)                        // decoder JSON
	if strict {                                           // mode ketat
		dec.DisallowUnknownFields() // tolak field yang tidak dikenal
	}
	if err := dec.Decode(dst); err != nil { // decode gagal
		var mbe *http.MaxBytesError // penampung error body kebesaran
		switch {
		case errors.Is(err, io.EOF): // body kosong
			if allowEmpty { // kosong diizinkan
				return nil
			}
			return ErrValidation("Body request kosong.", nil)
		case errors.As(err, &mbe): // body melebihi batas
			return ErrValidation("Body request terlalu besar (maks 1 MB).", nil)
		default: // JSON rusak/tipe salah
			return ErrValidation("Body JSON tidak valid: "+err.Error(), nil)
		}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) { // masih ada data setelah objek utama
		return ErrValidation("Body JSON memuat data tambahan setelah objek utama.", nil)
	}
	return nil // body valid
}

// QueryInt membaca query integer dengan default dan batas; tidak valid => VALIDATION_ERROR.
func QueryInt(r *http.Request, key string, def, min, max int) (int, error) {
	s := r.URL.Query().Get(key) // ambil nilai query
	if s == "" {                // tidak diberikan
		return def, nil // pakai default
	}
	n, err := strconv.Atoi(s)             // konversi ke integer
	if err != nil || n < min || n > max { // bukan angka atau di luar rentang
		return 0, ErrValidation("Parameter "+key+" harus integer "+strconv.Itoa(min)+"–"+strconv.Itoa(max)+".",
			[]FieldError{{Field: key, Message: "nilai tidak valid"}}) // laporkan field yang salah
	}
	return n, nil // nilai valid
}

// Pagination membaca page (>=1, default 1) dan limit (1..100, default 20).
func Pagination(r *http.Request) (page, limit int, err error) {
	if page, err = QueryInt(r, "page", 1, 1, 1_000_000); err != nil { // baca page
		return // kembalikan error
	}
	limit, err = QueryInt(r, "limit", 20, 1, 100) // baca limit
	return                                        // kembalikan page, limit, err
}

// QueryBool membaca query boolean ("true"/"false"); nil bila tidak ada.
func QueryBool(r *http.Request, key string) (*bool, error) {
	s := r.URL.Query().Get(key) // ambil nilai query
	if s == "" {                // tidak diberikan
		return nil, nil // nil = tidak difilter
	}
	b, err := strconv.ParseBool(s) // konversi ke bool
	if err != nil {                // bukan boolean valid
		return nil, ErrValidation("Parameter "+key+" harus true/false.", []FieldError{{Field: key, Message: "harus boolean"}})
	}
	return &b, nil // pointer ke nilai bool
}
