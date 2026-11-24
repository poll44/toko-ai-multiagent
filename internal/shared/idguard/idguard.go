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
package idguard // nama package: idguard

import ( // package yang dipakai
	"bytes"         // buffer byte untuk body baru
	"encoding/json" // encode/decode JSON
	"io"            // membaca & membungkus body
	"net/http"      // tipe HTTP
	"path"          // normalisasi path
	"regexp"        // regex rute sesi
	"strconv"       // konversi angka ke string

	"toko-ai-microservices/internal/shared/httputil" // helper HTTP bersama
)

// sessionsRe mencocokkan rute sesi: /apps/{app}/users/{uid}/sessions[/{sid}].
var sessionsRe = regexp.MustCompile(`^/apps/([^/]+)/users/([^/]+)/sessions(?:/([^/]+))?$`)

// identityKeys: kunci state yang hanya boleh diisi server.
var identityKeys = []string{"user_id", "user_role"}

// hasIdentityKey: true bila map memuat salah satu kunci identitas terlarang.
func hasIdentityKey(m map[string]any) bool {
	for _, k := range identityKeys { // periksa tiap kunci identitas
		if _, ok := m[k]; ok { // kunci ditemukan
			return true
		}
	}
	return false // tidak ada kunci identitas
}

// readBody membaca seluruh body dengan batas ukuran.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, httputil.MaxBodyBytes) // batasi ukuran body
	b, err := io.ReadAll(r.Body)                                   // baca semuanya
	if err != nil {                                                // gagal / terlalu besar
		return nil, httputil.ErrValidation("Body request tidak dapat dibaca atau terlalu besar.", nil)
	}
	return b, nil
}

// setBody mengganti body request dengan b dan menyesuaikan Content-Length.
func setBody(r *http.Request, b []byte) {
	r.Body = io.NopCloser(bytes.NewReader(b))            // body baru dari byte
	r.ContentLength = int64(len(b))                      // panjang body
	r.Header.Set("Content-Length", strconv.Itoa(len(b))) // sinkronkan header
}

// Enforce membungkus handler ADK. Harus berada SETELAH InternalAuthMiddleware + IdentityMiddleware.
func Enforce(appName string, next http.Handler) http.Handler {
	fail := func(w http.ResponseWriter, r *http.Request, err error) { httputil.Fail(w, r, err) } // jalan pintas menulis error
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := httputil.IdentityFrom(r.Context()) // identitas dari gateway
		p := path.Clean(r.URL.Path)              // path ternormalisasi
		if p != r.URL.Path {                     // tolak path ber-"..", "//" atau trailing slash agar tidak mengakali allowlist
			fail(w, r, httputil.ErrNotFound("Rute tidak ditemukan."))
			return
		}

		switch {
		case p == "/run" || p == "/run_sse": // rute menjalankan agent
			if r.Method != http.MethodPost { // hanya POST
				fail(w, r, httputil.NewError(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Gunakan POST.", nil))
				return
			}
			b, err := readBody(w, r) // baca body
			if err != nil {
				fail(w, r, err)
				return
			}
			var body map[string]any                          // body sebagai map
			if err := json.Unmarshal(b, &body); err != nil { // JSON tidak valid
				fail(w, r, httputil.ErrValidation("Body JSON tidak valid.", nil))
				return
			}
			if uid, _ := body["userId"].(string); uid != id.UserID { // userId harus sama dengan identitas
				fail(w, r, httputil.ErrForbidden("userId tidak sesuai dengan identitas Anda."))
				return
			}
			if app, _ := body["appName"].(string); app != appName { // appName harus sesuai
				fail(w, r, httputil.ErrNotFound("Aplikasi tidak ditemukan."))
				return
			}
			if sd, ok := body["stateDelta"].(map[string]any); ok && hasIdentityKey(sd) { // cegah manipulasi identitas
				fail(w, r, httputil.ErrForbidden("stateDelta tidak boleh mengubah identitas."))
				return
			}
			setBody(r, b) // pasang kembali body yang sudah dibaca

		case sessionsRe.MatchString(p): // rute manajemen sesi
			m := sessionsRe.FindStringSubmatch(p) // ambil grup regex
			app, uid, sid := m[1], m[2], m[3]     // nama app, user id, session id
			if app != appName {                   // app tidak dikenal
				fail(w, r, httputil.ErrNotFound("Aplikasi tidak ditemukan."))
				return
			}
			if uid != id.UserID { // user di path harus sama dengan identitas
				fail(w, r, httputil.ErrForbidden("userId tidak sesuai dengan identitas Anda."))
				return
			}
			switch r.Method { // perlakuan per metode HTTP
			case http.MethodGet: // baca sesi: tanpa pemeriksaan tambahan
			case http.MethodDelete: // hapus sesi
				if sid == "" { // wajib ada session id
					fail(w, r, httputil.NewError(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE memerlukan session id.", nil))
					return
				}
			case http.MethodPost: // create session (dengan/ tanpa id): state identitas DITIMPA dari header
				b, err := readBody(w, r) // baca body
				if err != nil {
					fail(w, r, err)
					return
				}
				body := map[string]any{}         // body default kosong
				if len(bytes.TrimSpace(b)) > 0 { // ada isi
					if err := json.Unmarshal(b, &body); err != nil { // JSON tidak valid
						fail(w, r, httputil.ErrValidation("Body JSON tidak valid.", nil))
						return
					}
				}
				state, _ := body["state"].(map[string]any) // ambil state klien
				if state == nil {                          // belum ada
					state = map[string]any{}
				}
				state["user_id"], state["user_role"] = id.UserID, id.Role // timpa identitas dari header gateway
				body["state"] = state                                     // pasang state aman
				nb, _ := json.Marshal(body)                               // encode ulang
				setBody(r, nb)                                            // ganti body request
				r.Header.Set("Content-Type", "application/json")          // pastikan tipe konten JSON
			case http.MethodPatch: // update state: tidak boleh menyentuh identitas
				if sid == "" { // wajib ada session id
					fail(w, r, httputil.NewError(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PATCH memerlukan session id.", nil))
					return
				}
				b, err := readBody(w, r) // baca body
				if err != nil {
					fail(w, r, err)
					return
				}
				var body map[string]any                          // body sebagai map
				if err := json.Unmarshal(b, &body); err != nil { // JSON tidak valid
					fail(w, r, httputil.ErrValidation("Body JSON tidak valid.", nil))
					return
				}
				if sd, ok := body["stateDelta"].(map[string]any); ok && hasIdentityKey(sd) { // cegah ubah identitas
					fail(w, r, httputil.ErrForbidden("stateDelta tidak boleh mengubah identitas."))
					return
				}
				setBody(r, b) // pasang kembali body
			default: // metode lain tidak didukung
				fail(w, r, httputil.NewError(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method tidak didukung.", nil))
				return
			}

		default: // rute di luar allowlist
			fail(w, r, httputil.ErrNotFound("Rute tidak ditemukan."))
			return
		}
		next.ServeHTTP(w, r) // lolos pemeriksaan, teruskan ke ADK
	})
}
