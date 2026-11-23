// Package middleware: middleware khusus Gateway (TRD 01 §7): CORS, sanitasi header, blokir /internal, JWT.
package middleware

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/service-gateway/internal/auth"
)

// ---- CORS ----

// CORS mengizinkan HANYA origin frontend (exact match). Preflight (OPTIONS + Origin) dijawab 204.
// Request tanpa Origin (curl, server-ke-server) tidak terpengaruh.
func CORS(allowedOrigin string) httputil.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				w.Header().Add("Vary", "Origin")
				if origin == allowedOrigin {
					h := w.Header()
					h.Set("Access-Control-Allow-Origin", origin)
					h.Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
					h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-request-id")
					h.Set("Access-Control-Expose-Headers", "x-request-id, x-search-mode")
					h.Set("Access-Control-Max-Age", "600")
				}
				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ---- sanitasi header identitas ----

// SanitizeIdentityHeaders menghapus header identitas buatan klien (anti-spoofing) sebelum Gateway menyuntik miliknya.
func SanitizeIdentityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, k := range []string{httputil.HeaderUserID, httputil.HeaderUserRole, httputil.HeaderInternalKey, httputil.HeaderServiceName} {
			r.Header.Del(k)
		}
		next.ServeHTTP(w, r)
	})
}

// ---- blokir /internal ----

// BlockInternalPaths menolak (404) path yang memuat segmen "internal" — termasuk bentuk ter-encode ganda.
func BlockInternalPaths(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		candidates := []string{r.URL.Path, r.URL.EscapedPath()}
		for _, p := range append([]string(nil), candidates...) {
			if u, err := url.PathUnescape(p); err == nil {
				candidates = append(candidates, u)
				if u2, err := url.PathUnescape(u); err == nil { // decode ganda (%252e)
					candidates = append(candidates, u2)
				}
			}
		}
		for _, p := range candidates {
			for _, seg := range strings.Split(strings.ToLower(p), "/") {
				if seg == "internal" {
					httputil.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Rute tidak ditemukan.", nil)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- autentikasi JWT ----

type claimsKey struct{}

// ClaimsFrom mengembalikan klaim JWT (nil bila GUEST).
func ClaimsFrom(ctx context.Context) *auth.Claims {
	c, _ := ctx.Value(claimsKey{}).(*auth.Claims)
	return c
}

// Authenticate: tanpa Authorization => GUEST/GUEST. Ada tetapi tidak valid/kedaluwarsa => 401 UNAUTHENTICATED
// (TIDAK diturunkan menjadi GUEST). Valid => Identity dari klaim. Identitas disimpan di context & dicatat untuk logging.
func Authenticate(secret string) httputil.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := httputil.Identity{UserID: httputil.RoleGuest, Role: httputil.RoleGuest, RequestID: httputil.RequestIDFrom(r.Context())}
			ctx := r.Context()
			if hdr := r.Header.Get("Authorization"); hdr != "" {
				scheme, token, ok := strings.Cut(hdr, " ")
				token = strings.TrimSpace(token)
				if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
					unauthenticated(w, r)
					return
				}
				claims, err := auth.Parse(secret, token)
				if err != nil || claims.Role == httputil.RoleGuest || !httputil.ValidRole(claims.Role) {
					unauthenticated(w, r)
					return
				}
				id.UserID, id.Role = claims.Subject, claims.Role
				ctx = context.WithValue(ctx, claimsKey{}, claims)
			}
			ctx = httputil.WithIdentity(ctx, id)
			httputil.RecordIdentity(ctx, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func unauthenticated(w http.ResponseWriter, r *http.Request) {
	httputil.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Token tidak ada, tidak valid, atau sudah kedaluwarsa.", nil)
}

// RequireAuth: GUEST => 401 UNAUTHENTICATED.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if httputil.IdentityFrom(r.Context()).Role == httputil.RoleGuest {
			unauthenticated(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRoles: GUEST => 401; role login lain di luar daftar => 403 FORBIDDEN (fail-fast; service domain tetap memvalidasi).
func RequireRoles(roles ...string) httputil.Middleware {
	return func(next http.Handler) http.Handler {
		return RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := httputil.IdentityFrom(r.Context()).Role
			for _, want := range roles {
				if role == want {
					next.ServeHTTP(w, r)
					return
				}
			}
			httputil.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Anda tidak berhak mengakses sumber daya ini.", nil)
		}))
	}
}

// Method membatasi method (selain itu 405 JSON standar; ServeMux tanpa pola method agar 405 tidak berupa teks polos).
func Method(method string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			httputil.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method tidak diizinkan. Gunakan "+method+".", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
