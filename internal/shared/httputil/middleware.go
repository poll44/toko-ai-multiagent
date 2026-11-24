package httputil // helper HTTP bersama

import ( // package yang dipakai
	"context"       // context request
	"crypto/rand"   // pembangkit byte acak aman
	"crypto/subtle" // perbandingan constant-time
	"fmt"           // format string
	"log/slog"      // logging terstruktur
	"net/http"      // tipe HTTP
	"regexp"        // regular expression
	"runtime/debug" // stack trace saat panic
	"slices"        // utilitas slice
	"time"          // pengukuran waktu
)

// Middleware adalah decorator http.Handler.
type Middleware = func(http.Handler) http.Handler

// Chain menyusun middleware; yang pertama adalah lapisan terluar.
func Chain(mw ...Middleware) Middleware {
	return func(next http.Handler) http.Handler { // fungsi yang membungkus handler akhir
		for i := len(mw) - 1; i >= 0; i-- { // bungkus dari yang terakhir agar yang pertama jadi terluar
			next = mw[i](next) // terapkan middleware ke handler saat ini
		}
		return next // handler hasil pembungkusan
	}
}

// uuidRe memvalidasi format UUID (8-4-4-4-12 heksadesimal).
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// NewUUID menghasilkan UUID v4 (crypto/rand).
func NewUUID() string {
	var b [16]byte                                                                  // 16 byte acak
	_, _ = rand.Read(b[:])                                                          // isi dengan byte acak
	b[6] = (b[6] & 0x0f) | 0x40                                                     // set versi = 4
	b[8] = (b[8] & 0x3f) | 0x80                                                     // set varian RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]) // format 8-4-4-4-12
}

// RequestID memakai x-request-id yang valid (UUID) atau membuat baru; menyimpan di context & header respons.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID) // ambil ID dari header masuk
		if !uuidRe.MatchString(id) {        // tidak ada / bukan UUID
			id = NewUUID() // buat ID baru
		}
		w.Header().Set(HeaderRequestID, id)                              // kembalikan ID di header respons
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id))) // teruskan dengan ID di context
	})
}

// Recover: panic -> 500 INTERNAL_ERROR (stack hanya ke log).
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { // dijalankan saat handler selesai/panic
			if rec := recover(); rec != nil { // ada panic
				slog.Error("panic", "recover", rec, "stack", string(debug.Stack()), "request_id", RequestIDFrom(r.Context())) // catat stack ke log
				e := ErrInternal()                                                                                            // siapkan error 500 generik
				WriteError(w, r, e.Status, e.Code, e.Message, nil)                                                            // kirim respons error
			}
		}()
		next.ServeHTTP(w, r) // jalankan handler berikutnya
	})
}

// statusRecorder membungkus ResponseWriter untuk merekam status code.
type statusRecorder struct {
	http.ResponseWriter     // writer asli yang dibungkus
	status              int // status code yang ditulis
}

// WriteHeader merekam status lalu meneruskannya ke writer asli.
func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

// Unwrap agar http.ResponseController (Flush untuk SSE) tetap bekerja.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Logging: log terstruktur per request (tanpa secret/header sensitif).
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()                                              // catat waktu mulai
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK} // recorder dengan status default 200
		// holder diisi IdentityMiddleware (context turunan tidak terlihat oleh middleware luar).
		holder := &Identity{}                                                                 // wadah identitas yang diisi middleware di dalam
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), holderKey, holder))) // jalankan handler dengan holder di context
		slog.Info("request",                                                                  // tulis log setelah request selesai
			"method", r.Method, "path", r.URL.Path, "status", rec.status, // metode, path, status
			"ms", time.Since(start).Milliseconds(), // durasi dalam milidetik
			"request_id", RequestIDFrom(r.Context()), "user_id", holder.UserID) // ID request & user
	})
}

// InternalAuthMiddleware: x-internal-api-key harus sama (constant time) -> 401 INVALID_INTERNAL_KEY.
func InternalAuthMiddleware(key string) Middleware {
	want := []byte(key) // kunci yang diharapkan
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := []byte(r.Header.Get(HeaderInternalKey))                    // kunci dari pemanggil
			if len(want) == 0 || subtle.ConstantTimeCompare(got, want) != 1 { // kunci kosong atau tidak cocok
				WriteError(w, r, http.StatusUnauthorized, "INVALID_INTERNAL_KEY", "Kunci internal tidak valid.", nil) // tolak 401
				return                                                                                                // hentikan proses
			}
			next.ServeHTTP(w, r) // kunci valid, lanjutkan
		})
	}
}

// IdentityMiddleware membaca x-user-id/x-user-role/x-service-name -> Identity di context.
func IdentityMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, role := r.Header.Get(HeaderUserID), r.Header.Get(HeaderUserRole) // ambil ID & role dari header
			if uid == "" || !ValidRole(role) {                                    // ID kosong atau role tidak dikenal
				WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Identitas pengguna tidak valid.", nil) // tolak 403
				return                                                                                      // hentikan proses
			}
			id := Identity{UserID: uid, Role: role, RequestID: RequestIDFrom(r.Context()), ServiceName: r.Header.Get(HeaderServiceName)} // rakit identitas
			RecordIdentity(r.Context(), id)                                                                                              // agar Logging (lapisan luar) dapat mencatat user_id
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))                                                              // lanjutkan dengan identitas di context
		})
	}
}

// RequireRole: role pemanggil harus salah satu dari roles -> 403 FORBIDDEN.
func RequireRole(roles ...string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !slices.Contains(roles, IdentityFrom(r.Context()).Role) { // role tidak termasuk yang diizinkan
				WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Anda tidak berhak mengakses sumber daya ini.", nil) // tolak 403
				return                                                                                                   // hentikan proses
			}
			next.ServeHTTP(w, r) // role diizinkan, lanjutkan
		})
	}
}

// DenyGuest: GUEST -> 403 FORBIDDEN.
func DenyGuest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IdentityFrom(r.Context()).Role == RoleGuest { // pemanggil adalah tamu
			WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Silakan login terlebih dahulu.", nil) // minta login
			return                                                                                     // hentikan proses
		}
		next.ServeHTTP(w, r) // bukan tamu, lanjutkan
	})
}

// RequireServiceCaller: x-service-name harus sama dengan name (untuk /internal/*).
func RequireServiceCaller(name string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IdentityFrom(r.Context()).ServiceName != name { // pemanggil bukan service yang berwenang
				WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Endpoint internal hanya untuk service yang berwenang.", nil) // tolak 403
				return                                                                                                            // hentikan proses
			}
			next.ServeHTTP(w, r) // service berwenang, lanjutkan
		})
	}
}
