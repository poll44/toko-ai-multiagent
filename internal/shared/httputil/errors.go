package httputil // helper HTTP bersama

import "net/http" // konstanta status HTTP

// FieldError: satu kesalahan validasi (details: [{field, message}]).
type FieldError struct {
	Field   string `json:"field"`   // nama field yang salah
	Message string `json:"message"` // pesan kesalahan
}

// APIError adalah error yang siap dipetakan ke respons error standar (Master TRD §7.2).
type APIError struct {
	Status  int    // kode status HTTP
	Code    string // kode error mesin (mis. NOT_FOUND)
	Message string // pesan untuk manusia
	Details any    // detail tambahan (mis. daftar FieldError)
}

// Error mengimplementasikan interface error.
func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// NewError membuat APIError generik dengan status, kode, pesan, dan detail.
func NewError(status int, code, message string, details any) *APIError {
	return &APIError{Status: status, Code: code, Message: message, Details: details} // rakit struct error
}

// ErrValidation: 400 untuk input yang tidak valid.
func ErrValidation(message string, details any) *APIError {
	return NewError(http.StatusBadRequest, "VALIDATION_ERROR", message, details)
}

// ErrUnauthenticated: 401 untuk pemanggil yang belum terautentikasi.
func ErrUnauthenticated(message string) *APIError {
	return NewError(http.StatusUnauthorized, "UNAUTHENTICATED", message, nil)
}

// ErrForbidden: 403 untuk pemanggil tanpa hak akses.
func ErrForbidden(message string) *APIError {
	return NewError(http.StatusForbidden, "FORBIDDEN", message, nil)
}

// ErrNotFound: 404 untuk resource yang tidak ditemukan.
func ErrNotFound(message string) *APIError {
	return NewError(http.StatusNotFound, "NOT_FOUND", message, nil)
}

// ErrConflict: 409 untuk konflik state, dengan kode error kustom.
func ErrConflict(code, message string, details any) *APIError {
	return NewError(http.StatusConflict, code, message, details)
}

// ErrInternal: 500 generik tanpa membocorkan detail internal.
func ErrInternal() *APIError {
	return NewError(http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server.", nil)
}
