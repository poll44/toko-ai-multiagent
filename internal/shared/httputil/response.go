package httputil

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Meta: objek `meta` pada respons daftar (total/page/limit + field tambahan seperti mode, query).
type Meta map[string]any

func PageMeta(total, page, limit int) Meta {
	return Meta{"total": total, "page": page, "limit": limit}
}

// WriteJSON menulis JSON dengan status. Status 204 tanpa body.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("gagal menulis respons JSON", "err", err)
	}
}

// WriteData: {"data": ...}
func WriteData(w http.ResponseWriter, status int, data any) {
	WriteJSON(w, status, map[string]any{"data": data})
}

// WriteList: {"data": [...], "meta": {...}} (200). Pastikan data bukan nil slice agar tampil [] bukan null.
func WriteList(w http.ResponseWriter, data any, meta Meta) {
	WriteJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details"`
	RequestID string `json:"request_id"`
}

// WriteError: {"error": {code, message, details, request_id}}
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	WriteJSON(w, status, map[string]any{"error": errorBody{
		Code: code, Message: message, Details: details, RequestID: RequestIDFrom(r.Context()),
	}})
}

// Fail memetakan *APIError -> respons standar; error lain -> 500 INTERNAL_ERROR (detail hanya ke log).
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *APIError
	if errors.As(err, &ae) {
		WriteError(w, r, ae.Status, ae.Code, ae.Message, ae.Details)
		return
	}
	slog.Error("error tak tertangani", "err", err, "request_id", RequestIDFrom(r.Context()), "path", r.URL.Path)
	e := ErrInternal()
	WriteError(w, r, e.Status, e.Code, e.Message, nil)
}

// NotImplemented: handler placeholder untuk kerangka (501).
func NotImplemented(w http.ResponseWriter, r *http.Request) {
	WriteError(w, r, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Endpoint belum diimplementasikan.", nil)
}

// Health: GET /health -> {"data":{"status":"ok","service":"..."}}
func Health(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteData(w, http.StatusOK, map[string]string{"status": "ok", "service": service})
	}
}
