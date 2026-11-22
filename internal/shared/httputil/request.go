package httputil

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
)

// MaxBodyBytes: batas body request (1 MB).
const MaxBodyBytes = 1 << 20

// ReadJSON decode body JSON ke dst. Body kosong => VALIDATION_ERROR. Field tak dikenal diabaikan.
func ReadJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return readJSON(w, r, dst, false, false)
}

// ReadJSONStrict seperti ReadJSON tetapi menolak field tak dikenal.
func ReadJSONStrict(w http.ResponseWriter, r *http.Request, dst any) error {
	return readJSON(w, r, dst, true, false)
}

// ReadJSONOptional seperti ReadJSON tetapi body kosong dianggap valid (dst tak diubah).
func ReadJSONOptional(w http.ResponseWriter, r *http.Request, dst any) error {
	return readJSON(w, r, dst, false, true)
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any, strict, allowEmpty bool) error {
	if r.ContentLength != 0 {
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mt != "application/json" {
			return ErrValidation("Content-Type harus application/json.", nil)
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			if allowEmpty {
				return nil
			}
			return ErrValidation("Body request kosong.", nil)
		case errors.As(err, &mbe):
			return ErrValidation("Body request terlalu besar (maks 1 MB).", nil)
		default:
			return ErrValidation("Body JSON tidak valid: "+err.Error(), nil)
		}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrValidation("Body JSON memuat data tambahan setelah objek utama.", nil)
	}
	return nil
}

// QueryInt membaca query integer dengan default dan batas; tidak valid => VALIDATION_ERROR.
func QueryInt(r *http.Request, key string, def, min, max int) (int, error) {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > max {
		return 0, ErrValidation("Parameter "+key+" harus integer "+strconv.Itoa(min)+"–"+strconv.Itoa(max)+".",
			[]FieldError{{Field: key, Message: "nilai tidak valid"}})
	}
	return n, nil
}

// Pagination membaca page (>=1, default 1) dan limit (1..100, default 20).
func Pagination(r *http.Request) (page, limit int, err error) {
	if page, err = QueryInt(r, "page", 1, 1, 1_000_000); err != nil {
		return
	}
	limit, err = QueryInt(r, "limit", 20, 1, 100)
	return
}

// QueryBool membaca query boolean ("true"/"false"); nil bila tidak ada.
func QueryBool(r *http.Request, key string) (*bool, error) {
	s := r.URL.Query().Get(key)
	if s == "" {
		return nil, nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return nil, ErrValidation("Parameter "+key+" harus true/false.", []FieldError{{Field: key, Message: "harus boolean"}})
	}
	return &b, nil
}
