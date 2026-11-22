// Package idguard: middleware di depan server REST ADK yang MENEGAKKAN identitas sendiri (defense in depth;
// Gateway juga melakukannya). ADK REST API tidak berautentikasi dan seluruh endpoint-nya membaca/menulis
// sesi, jadi service ini hanya mengizinkan allowlist rute dan memaksa identitas sesi = header Gateway.
//
// Rute ADK yang diizinkan:
//
//	POST  /run, /run_sse                                  -> body.userId harus = x-user-id; stateDelta tak boleh memuat kunci identitas
//	POST  /apps/{app}/users/{uid}/sessions                -> state.user_id/user_role DITIMPA dari header
//	POST  /apps/{app}/users/{uid}/sessions/{sid}          -> idem
//	PATCH /apps/{app}/users/{uid}/sessions/{sid}          -> stateDelta tak boleh memuat kunci identitas
//	GET   /apps/{app}/users/{uid}/sessions[/{sid}], DELETE .../sessions/{sid}
//
// Semua rute lain (/list-apps, /debug/*, /eval*, /run_live, /dev-ui, ...) -> 404.
package idguard

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"regexp"
	"strconv"

	"toko-ai-microservices/internal/shared/httputil"
)

var sessionsRe = regexp.MustCompile(`^/apps/([^/]+)/users/([^/]+)/sessions(?:/([^/]+))?$`)

// identityKeys: kunci state yang hanya boleh diisi server.
var identityKeys = []string{"user_id", "user_role"}

func hasIdentityKey(m map[string]any) bool {
	for _, k := range identityKeys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, httputil.ErrValidation("Body request tidak dapat dibaca atau terlalu besar.", nil)
	}
	return b, nil
}

func setBody(r *http.Request, b []byte) {
	r.Body = io.NopCloser(bytes.NewReader(b))
	r.ContentLength = int64(len(b))
	r.Header.Set("Content-Length", strconv.Itoa(len(b)))
}

// Enforce membungkus handler ADK. Harus berada SETELAH InternalAuthMiddleware + IdentityMiddleware.
func Enforce(appName string, next http.Handler) http.Handler {
	fail := func(w http.ResponseWriter, r *http.Request, err error) { httputil.Fail(w, r, err) }
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := httputil.IdentityFrom(r.Context())
		p := path.Clean(r.URL.Path)
		if p != r.URL.Path { // tolak path ber-"..", "//" atau trailing slash agar tidak mengakali allowlist
			fail(w, r, httputil.ErrNotFound("Rute tidak ditemukan."))
			return
		}

		switch {
		case p == "/run" || p == "/run_sse":
			if r.Method != http.MethodPost {
				fail(w, r, httputil.NewError(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Gunakan POST.", nil))
				return
			}
			b, err := readBody(w, r)
			if err != nil {
				fail(w, r, err)
				return
			}
			var body map[string]any
			if err := json.Unmarshal(b, &body); err != nil {
				fail(w, r, httputil.ErrValidation("Body JSON tidak valid.", nil))
				return
			}
			if uid, _ := body["userId"].(string); uid != id.UserID {
				fail(w, r, httputil.ErrForbidden("userId tidak sesuai dengan identitas Anda."))
				return
			}
			if app, _ := body["appName"].(string); app != appName {
				fail(w, r, httputil.ErrNotFound("Aplikasi tidak ditemukan."))
				return
			}
			if sd, ok := body["stateDelta"].(map[string]any); ok && hasIdentityKey(sd) {
				fail(w, r, httputil.ErrForbidden("stateDelta tidak boleh mengubah identitas."))
				return
			}
			setBody(r, b)

		case sessionsRe.MatchString(p):
			m := sessionsRe.FindStringSubmatch(p)
			app, uid, sid := m[1], m[2], m[3]
			if app != appName {
				fail(w, r, httputil.ErrNotFound("Aplikasi tidak ditemukan."))
				return
			}
			if uid != id.UserID {
				fail(w, r, httputil.ErrForbidden("userId tidak sesuai dengan identitas Anda."))
				return
			}
			switch r.Method {
			case http.MethodGet:
			case http.MethodDelete:
				if sid == "" {
					fail(w, r, httputil.NewError(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE memerlukan session id.", nil))
					return
				}
			case http.MethodPost: // create session (dengan/ tanpa id): state identitas DITIMPA dari header
				b, err := readBody(w, r)
				if err != nil {
					fail(w, r, err)
					return
				}
				body := map[string]any{}
				if len(bytes.TrimSpace(b)) > 0 {
					if err := json.Unmarshal(b, &body); err != nil {
						fail(w, r, httputil.ErrValidation("Body JSON tidak valid.", nil))
						return
					}
				}
				state, _ := body["state"].(map[string]any)
				if state == nil {
					state = map[string]any{}
				}
				state["user_id"], state["user_role"] = id.UserID, id.Role
				body["state"] = state
				nb, _ := json.Marshal(body)
				setBody(r, nb)
				r.Header.Set("Content-Type", "application/json")
			case http.MethodPatch: // update state: tidak boleh menyentuh identitas
				if sid == "" {
					fail(w, r, httputil.NewError(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PATCH memerlukan session id.", nil))
					return
				}
				b, err := readBody(w, r)
				if err != nil {
					fail(w, r, err)
					return
				}
				var body map[string]any
				if err := json.Unmarshal(b, &body); err != nil {
					fail(w, r, httputil.ErrValidation("Body JSON tidak valid.", nil))
					return
				}
				if sd, ok := body["stateDelta"].(map[string]any); ok && hasIdentityKey(sd) {
					fail(w, r, httputil.ErrForbidden("stateDelta tidak boleh mengubah identitas."))
					return
				}
				setBody(r, b)
			default:
				fail(w, r, httputil.NewError(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method tidak didukung.", nil))
				return
			}

		default:
			fail(w, r, httputil.ErrNotFound("Rute tidak ditemukan."))
			return
		}
		next.ServeHTTP(w, r)
	})
}
