package httputil

import "net/http"

// FieldError: satu kesalahan validasi (details: [{field, message}]).
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// APIError adalah error yang siap dipetakan ke respons error standar (Master TRD §7.2).
type APIError struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

func NewError(status int, code, message string, details any) *APIError {
	return &APIError{Status: status, Code: code, Message: message, Details: details}
}

func ErrValidation(message string, details any) *APIError {
	return NewError(http.StatusBadRequest, "VALIDATION_ERROR", message, details)
}
func ErrUnauthenticated(message string) *APIError {
	return NewError(http.StatusUnauthorized, "UNAUTHENTICATED", message, nil)
}
func ErrForbidden(message string) *APIError {
	return NewError(http.StatusForbidden, "FORBIDDEN", message, nil)
}
func ErrNotFound(message string) *APIError {
	return NewError(http.StatusNotFound, "NOT_FOUND", message, nil)
}
func ErrConflict(code, message string, details any) *APIError {
	return NewError(http.StatusConflict, code, message, details)
}
func ErrInternal() *APIError {
	return NewError(http.StatusInternalServerError, "INTERNAL_ERROR", "Terjadi kesalahan pada server.", nil)
}
