package httputil // helper HTTP bersama

import ( // package yang dipakai
	"encoding/json" // encode JSON
	"errors"        // errors.As
	"log/slog"      // logging terstruktur
	"net/http"      // tipe HTTP
)

// Meta: objek `meta` pada respons daftar (total/page/limit + field tambahan seperti mode, query).
type Meta map[string]any

// PageMeta membuat Meta paginasi standar.
func PageMeta(total, page, limit int) Meta {
	return Meta{"total": total, "page": page, "limit": limit} // total data, halaman, ukuran halaman
}

// WriteJSON menulis JSON dengan status. Status 204 tanpa body.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	if status == http.StatusNoContent { // 204 tidak boleh punya body
		w.WriteHeader(status) // tulis status saja
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8") // set tipe konten
	w.WriteHeader(status)                                             // tulis status
	if err := json.NewEncoder(w).Encode(v); err != nil {              // encode & kirim body
		slog.Error("gagal menulis respons JSON", "err", err) // catat bila gagal
	}
}

// WriteData: {"data": ...}
func WriteData(w http.ResponseWriter, status int, data any) {
	WriteJSON(w, status, map[string]any{"data": data}) // bungkus dalam kunci "data"
}

// WriteList: {"data": [...], "meta": {...}} (200). Pastikan data bukan nil slice agar tampil [] bukan null.
func WriteList(w http.ResponseWriter, data any, meta Meta) {
	WriteJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta}) // data + meta paginasi
}

// errorBody adalah isi objek "error" pada respons gagal.
type errorBody struct {
	Code      string `json:"code"`       // kode error mesin
	Message   string `json:"message"`    // pesan manusia
	Details   any    `json:"details"`    // detail tambahan
	RequestID string `json:"request_id"` // ID request untuk pelacakan
}

// WriteError: {"error": {code, message, details, request_id}}
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	WriteJSON(w, status, map[string]any{"error": errorBody{ // bungkus dalam kunci "error"
		Code: code, Message: message, Details: details, RequestID: RequestIDFrom(r.Context()), // isi field error
	}})
}

// Fail memetakan *APIError -> respons standar; error lain -> 500 INTERNAL_ERROR (detail hanya ke log).
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var ae *APIError         // penampung APIError
	if errors.As(err, &ae) { // error adalah APIError
		WriteError(w, r, ae.Status, ae.Code, ae.Message, ae.Details) // kirim sesuai isinya
		return
	}
	slog.Error("error tak tertangani", "err", err, "request_id", RequestIDFrom(r.Context()), "path", r.URL.Path) // catat detail ke log
	e := ErrInternal()                                                                                           // error 500 generik
	WriteError(w, r, e.Status, e.Code, e.Message, nil)                                                           // jangan bocorkan detail ke klien
}

// NotImplemented: handler placeholder untuk kerangka (501).
func NotImplemented(w http.ResponseWriter, r *http.Request) {
	WriteError(w, r, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Endpoint belum diimplementasikan.", nil)
}

// Health: GET /health -> {"data":{"status":"ok","service":"..."}}
func Health(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteData(w, http.StatusOK, map[string]string{"status": "ok", "service": service}) // laporkan status sehat
	}
}
