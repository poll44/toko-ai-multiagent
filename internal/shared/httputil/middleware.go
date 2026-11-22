package httputil

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"slices"
	"time"
)

// Middleware adalah decorator http.Handler.
type Middleware = func(http.Handler) http.Handler

// Chain menyusun middleware; yang pertama adalah lapisan terluar.
func Chain(mw ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(mw) - 1; i >= 0; i-- {
			next = mw[i](next)
		}
		return next
	}
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// NewUUID menghasilkan UUID v4 (crypto/rand).
func NewUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// RequestID memakai x-request-id yang valid (UUID) atau membuat baru; menyimpan di context & header respons.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if !uuidRe.MatchString(id) {
			id = NewUUID()
		}
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
	})
}

// Recover: panic -> 500 INTERNAL_ERROR (stack hanya ke log).
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "recover", rec, "stack", string(debug.Stack()), "request_id", RequestIDFrom(r.Context()))
				e := ErrInternal()
				WriteError(w, r, e.Status, e.Code, e.Message, nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

// Unwrap agar http.ResponseController (Flush untuk SSE) tetap bekerja.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Logging: log terstruktur per request (tanpa secret/header sensitif).
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		// holder diisi IdentityMiddleware (context turunan tidak terlihat oleh middleware luar).
		holder := &Identity{}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), holderKey, holder)))
		slog.Info("request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFrom(r.Context()), "user_id", holder.UserID)
	})
}

// InternalAuthMiddleware: x-internal-api-key harus sama (constant time) -> 401 INVALID_INTERNAL_KEY.
func InternalAuthMiddleware(key string) Middleware {
	want := []byte(key)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := []byte(r.Header.Get(HeaderInternalKey))
			if len(want) == 0 || subtle.ConstantTimeCompare(got, want) != 1 {
				WriteError(w, r, http.StatusUnauthorized, "INVALID_INTERNAL_KEY", "Kunci internal tidak valid.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IdentityMiddleware membaca x-user-id/x-user-role/x-service-name -> Identity di context.
func IdentityMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, role := r.Header.Get(HeaderUserID), r.Header.Get(HeaderUserRole)
			if uid == "" || !ValidRole(role) {
				WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Identitas pengguna tidak valid.", nil)
				return
			}
			id := Identity{UserID: uid, Role: role, RequestID: RequestIDFrom(r.Context()), ServiceName: r.Header.Get(HeaderServiceName)}
			RecordIdentity(r.Context(), id) // agar Logging (lapisan luar) dapat mencatat user_id
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}

// RequireRole: role pemanggil harus salah satu dari roles -> 403 FORBIDDEN.
func RequireRole(roles ...string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !slices.Contains(roles, IdentityFrom(r.Context()).Role) {
				WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Anda tidak berhak mengakses sumber daya ini.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// DenyGuest: GUEST -> 403 FORBIDDEN.
func DenyGuest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IdentityFrom(r.Context()).Role == RoleGuest {
			WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Silakan login terlebih dahulu.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireServiceCaller: x-service-name harus sama dengan name (untuk /internal/*).
func RequireServiceCaller(name string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IdentityFrom(r.Context()).ServiceName != name {
				WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Endpoint internal hanya untuk service yang berwenang.", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
