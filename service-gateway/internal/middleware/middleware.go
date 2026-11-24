// Package middleware: middleware khusus Gateway (TRD 01 §7): CORS, sanitasi header, blokir /internal, JWT.
package middleware // nama package: middleware

import ( // package yang dipakai
	"context"  // context request
	"net/http" // tipe HTTP
	"net/url"  // decode path
	"strings"  // utilitas string

	"toko-ai-microservices/internal/shared/httputil"      // helper HTTP bersama
	"toko-ai-microservices/service-gateway/internal/auth" // verifikasi JWT
)

// ---- CORS ----

// CORS mengizinkan HANYA origin frontend (exact match). Preflight (OPTIONS + Origin) dijawab 204.
// Request tanpa Origin (curl, server-ke-server) tidak terpengaruh.
func CORS(allowedOrigin string) httputil.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin") // origin pemanggil
			if origin != "" {                // request dari browser
				w.Header().Add("Vary", "Origin") // cache harus membedakan per origin
				if origin == allowedOrigin {     // hanya origin frontend yang diizinkan
					h := w.Header()
					h.Set("Access-Control-Allow-Origin", origin)                                       // izinkan origin ini
					h.Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")         // metode diizinkan
					h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-request-id") // header diizinkan
					h.Set("Access-Control-Expose-Headers", "x-request-id, x-search-mode")              // header yang boleh dibaca frontend
					h.Set("Access-Control-Max-Age", "600")                                             // cache preflight 10 menit
				}
				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" { // preflight
					w.WriteHeader(http.StatusNoContent) // jawab 204 tanpa body
					return
				}
			}
			next.ServeHTTP(w, r) // lanjutkan
		})
	}
}

// ---- sanitasi header identitas ----

// SanitizeIdentityHeaders menghapus header identitas buatan klien (anti-spoofing) sebelum Gateway menyuntik miliknya.
func SanitizeIdentityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, k := range []string{httputil.HeaderUserID, httputil.HeaderUserRole, httputil.HeaderInternalKey, httputil.HeaderServiceName} { // header sensitif
			r.Header.Del(k) // hapus dari request klien
		}
		next.ServeHTTP(w, r)
	})
}

// ---- blokir /internal ----

// BlockInternalPaths menolak (404) path yang memuat segmen "internal" — termasuk bentuk ter-encode ganda.
func BlockInternalPaths(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		candidates := []string{r.URL.Path, r.URL.EscapedPath()}  // bentuk path yang diperiksa
		for _, p := range append([]string(nil), candidates...) { // salinan agar aman saat menambah
			if u, err := url.PathUnescape(p); err == nil { // decode sekali
				candidates = append(candidates, u)
				if u2, err := url.PathUnescape(u); err == nil { // decode ganda (%252e)
					candidates = append(candidates, u2)
				}
			}
		}
		for _, p := range candidates { // periksa semua bentuk
			for _, seg := range strings.Split(strings.ToLower(p), "/") { // per segmen path
				if seg == "internal" { // ada segmen "internal"
					httputil.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Rute tidak ditemukan.", nil) // sembunyikan sebagai 404
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- autentikasi JWT ----

// claimsKey: kunci privat untuk menyimpan klaim JWT di context.
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
			id := httputil.Identity{UserID: httputil.RoleGuest, Role: httputil.RoleGuest, RequestID: httputil.RequestIDFrom(r.Context())} // default: tamu
			ctx := r.Context()
			if hdr := r.Header.Get("Authorization"); hdr != "" { // ada header Authorization
				scheme, token, ok := strings.Cut(hdr, " ") // pisahkan "Bearer <token>"
				token = strings.TrimSpace(token)
				if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" { // format salah
					unauthenticated(w, r)
					return
				}
				claims, err := auth.Parse(secret, token)                                                 // verifikasi JWT
				if err != nil || claims.Role == httputil.RoleGuest || !httputil.ValidRole(claims.Role) { // token buruk / role tak sah
					unauthenticated(w, r)
					return
				}
				id.UserID, id.Role = claims.Subject, claims.Role  // identitas dari klaim
				ctx = context.WithValue(ctx, claimsKey{}, claims) // simpan klaim
			}
			ctx = httputil.WithIdentity(ctx, id) // simpan identitas di context
			httputil.RecordIdentity(ctx, id)     // catat untuk log request
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// unauthenticated menulis respons 401 UNAUTHENTICATED standar.
func unauthenticated(w http.ResponseWriter, r *http.Request) {
	httputil.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "Token tidak ada, tidak valid, atau sudah kedaluwarsa.", nil)
}

// RequireAuth: GUEST => 401 UNAUTHENTICATED.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if httputil.IdentityFrom(r.Context()).Role == httputil.RoleGuest { // belum login
			unauthenticated(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRoles: GUEST => 401; role login lain di luar daftar => 403 FORBIDDEN (fail-fast; service domain tetap memvalidasi).
func RequireRoles(roles ...string) httputil.Middleware {
	return func(next http.Handler) http.Handler {
		return RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { // wajib login dulu
			role := httputil.IdentityFrom(r.Context()).Role // role pemanggil
			for _, want := range roles {                    // cocokkan dengan daftar yang diizinkan
				if role == want {
					next.ServeHTTP(w, r)
					return
				}
			}
			httputil.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "Anda tidak berhak mengakses sumber daya ini.", nil) // role tidak diizinkan
		}))
	}
}

// Method membatasi method (selain itu 405 JSON standar; ServeMux tanpa pola method agar 405 tidak berupa teks polos).
func Method(method string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method { // metode tidak sesuai
			w.Header().Set("Allow", method) // beri tahu metode yang boleh
			httputil.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method tidak diizinkan. Gunakan "+method+".", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
