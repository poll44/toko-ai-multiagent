package idguard_test // test black-box untuk package idguard

import ( // package yang dipakai
	"context"           // context
	"encoding/json"     // decode body yang ditangkap
	"io"                // membaca body
	"net/http"          // tipe HTTP
	"net/http/httptest" // recorder & request uji
	"strings"           // membuat body dari string
	"testing"           // framework test

	"toko-ai-microservices/internal/shared/httputil" // identitas
	"toko-ai-microservices/internal/shared/idguard"  // paket yang diuji
)

// app: nama aplikasi ADK yang diizinkan pada test.
const app = "toko_orchestrator"

// captured: rekaman request yang berhasil sampai ke handler ADK (next).
type captured struct {
	called bool           // apakah handler ADK dipanggil
	body   map[string]any // isi body yang diterima
	method string         // metode HTTP yang diterima
	path   string         // path yang diterima
}

// run menjalankan Enforce dengan identitas id, lalu mengembalikan respons & rekaman request ke ADK.
func run(t *testing.T, id httputil.Identity, method, path, body string) (*httptest.ResponseRecorder, *captured) {
	t.Helper()
	cap := &captured{}                                                      // rekaman
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { // handler ADK palsu
		cap.called, cap.method, cap.path = true, r.Method, r.URL.Path // catat pemanggilan
		b, _ := io.ReadAll(r.Body)                                    // baca body yang diteruskan
		if len(b) > 0 {
			_ = json.Unmarshal(b, &cap.body) // simpan sebagai map
		}
		w.WriteHeader(200)
	})
	h := idguard.Enforce(app, next)                                        // bungkus dengan idguard
	req := httptest.NewRequest(method, path, strings.NewReader(body))      // request uji
	req = req.WithContext(httputil.WithIdentity(context.Background(), id)) // pasang identitas
	rec := httptest.NewRecorder()                                          // perekam respons
	h.ServeHTTP(rec, req)                                                  // jalankan
	return rec, cap
}

// budi: identitas pelanggan yang dipakai di banyak test.
var budi = httputil.Identity{UserID: "USR-01", Role: "CUSTOMER"}

// errCode mengambil kode error dari body respons.
func errCode(rec *httptest.ResponseRecorder) string {
	var m struct {
		Error struct{ Code string } `json:"error"` // amplop error standar
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return m.Error.Code
}

// TestCreateSessionOverwritesIdentityState: state identitas pada pembuatan sesi harus ditimpa dari header.
func TestCreateSessionOverwritesIdentityState(t *testing.T) {
	for _, p := range []string{ // dua bentuk rute pembuatan sesi
		"/apps/toko_orchestrator/users/USR-01/sessions",
		"/apps/toko_orchestrator/users/USR-01/sessions/s-123",
	} {
		// klien mencoba menyisipkan identitas palsu + state kustom
		rec, c := run(t, budi, "POST", p, `{"state":{"user_id":"ADM-01","user_role":"ADMIN_SISTEM","theme":"dark"}}`)
		if rec.Code != 200 || !c.called { // harus lolos ke ADK
			t.Fatalf("%s: status %d", p, rec.Code)
		}
		st := c.body["state"].(map[string]any)                          // state yang sampai ke ADK
		if st["user_id"] != "USR-01" || st["user_role"] != "CUSTOMER" { // identitas harus dari header
			t.Fatalf("%s: identitas harus ditimpa dari header: %v", p, st)
		}
		if st["theme"] != "dark" { // state kustom dipertahankan
			t.Fatalf("%s: state kustom harus dipertahankan: %v", p, st)
		}
	}
	// body kosong => state identitas tetap disuntik
	_, c := run(t, budi, "POST", "/apps/toko_orchestrator/users/USR-01/sessions/s-1", "")
	if st, _ := c.body["state"].(map[string]any); st["user_id"] != "USR-01" {
		t.Fatalf("body kosong: %v", c.body)
	}
	// GUEST
	_, c = run(t, httputil.Identity{UserID: "GUEST", Role: "GUEST"}, "POST", "/apps/toko_orchestrator/users/GUEST/sessions/s-2", `{}`)
	if st := c.body["state"].(map[string]any); st["user_role"] != "GUEST" {
		t.Fatalf("guest: %v", st)
	}
}

// TestUserIDMismatchForbidden: userId pada path/body yang berbeda dari identitas harus ditolak 403.
func TestUserIDMismatchForbidden(t *testing.T) {
	cases := []struct{ method, path, body string }{ // kasus yang harus ditolak
		{"POST", "/apps/toko_orchestrator/users/USR-02/sessions", `{}`},
		{"GET", "/apps/toko_orchestrator/users/USR-02/sessions/s-1", ""},
		{"DELETE", "/apps/toko_orchestrator/users/USR-02/sessions/s-1", ""},
		{"POST", "/run", `{"appName":"toko_orchestrator","userId":"USR-02","sessionId":"s","newMessage":{}}`},
		{"POST", "/run_sse", `{"appName":"toko_orchestrator","userId":"ADM-01","sessionId":"s","newMessage":{}}`},
		{"POST", "/run", `{"appName":"toko_orchestrator","sessionId":"s","newMessage":{}}`}, // userId hilang
	}
	for _, c := range cases {
		rec, cap := run(t, budi, c.method, c.path, c.body)
		if rec.Code != 403 || errCode(rec) != "FORBIDDEN" || cap.called { // harus 403 & ADK tidak tersentuh
			t.Errorf("%s %s: harus 403 FORBIDDEN tanpa menyentuh ADK, dapat %d %s called=%v", c.method, c.path, rec.Code, errCode(rec), cap.called)
		}
	}
}

// TestStateDeltaIdentityRejected: stateDelta yang memuat kunci identitas harus ditolak.
func TestStateDeltaIdentityRejected(t *testing.T) {
	rec, cap := run(t, budi, "POST", "/run", `{"appName":"toko_orchestrator","userId":"USR-01","sessionId":"s","newMessage":{},"stateDelta":{"user_role":"ADMIN_SISTEM"}}`)
	if rec.Code != 403 || cap.called {
		t.Fatalf("/run stateDelta identitas: %d called=%v", rec.Code, cap.called)
	}
	rec, cap = run(t, budi, "PATCH", "/apps/toko_orchestrator/users/USR-01/sessions/s-1", `{"stateDelta":{"user_id":"ADM-01"}}`)
	if rec.Code != 403 || cap.called {
		t.Fatalf("PATCH stateDelta identitas: %d called=%v", rec.Code, cap.called)
	}
	// stateDelta non-identitas tetap boleh
	rec, cap = run(t, budi, "PATCH", "/apps/toko_orchestrator/users/USR-01/sessions/s-1", `{"stateDelta":{"theme":"dark"}}`)
	if rec.Code != 200 || !cap.called {
		t.Fatalf("PATCH stateDelta non-identitas harus lolos: %d", rec.Code)
	}
	rec, _ = run(t, budi, "POST", "/run", `{"appName":"toko_orchestrator","userId":"USR-01","sessionId":"s","newMessage":{"role":"user","parts":[{"text":"hai"}]}}`)
	if rec.Code != 200 {
		t.Fatalf("/run valid harus lolos: %d", rec.Code)
	}
}

// TestRouteAllowlist: rute di luar allowlist harus 404, dan metode salah harus 405.
func TestRouteAllowlist(t *testing.T) {
	for _, p := range []string{ // rute yang harus ditolak 404
		"/list-apps", "/debug/trace/x", "/run_live", "/dev-ui", "/docs", "/openapi.json",
		"/apps/toko_orchestrator/eval_sets", "/apps/toko_orchestrator",
		"/apps/toko_orchestrator/users/USR-01/sessions/s-1/events",
		"/apps/../list-apps", "//run", "/run/",
	} {
		rec, cap := run(t, budi, "GET", p, "")
		if rec.Code != 404 || cap.called {
			t.Errorf("%s harus 404 tanpa menyentuh ADK, dapat %d called=%v", p, rec.Code, cap.called)
		}
	}
	// aplikasi lain
	rec, _ := run(t, budi, "POST", "/apps/lain/users/USR-01/sessions", `{}`)
	if rec.Code != 404 {
		t.Fatalf("app lain: %d", rec.Code)
	}
	rec, _ = run(t, budi, "POST", "/run", `{"appName":"lain","userId":"USR-01","sessionId":"s","newMessage":{}}`)
	if rec.Code != 404 {
		t.Fatalf("appName lain pada /run: %d", rec.Code)
	}
	// method salah
	rec, _ = run(t, budi, "GET", "/run", "")
	if rec.Code != 405 {
		t.Fatalf("GET /run: %d", rec.Code)
	}
	rec, _ = run(t, budi, "DELETE", "/apps/toko_orchestrator/users/USR-01/sessions", "")
	if rec.Code != 405 {
		t.Fatalf("DELETE tanpa session id: %d", rec.Code)
	}
}

// TestMalformedBodies: body yang bukan JSON valid harus ditolak 400.
func TestMalformedBodies(t *testing.T) {
	for _, c := range []struct{ method, path, body string }{ // kasus body rusak
		{"POST", "/run", `not json`},
		{"POST", "/apps/toko_orchestrator/users/USR-01/sessions", `{bad`},
		{"PATCH", "/apps/toko_orchestrator/users/USR-01/sessions/s-1", `[]`},
	} {
		rec, cap := run(t, budi, c.method, c.path, c.body)
		if rec.Code != 400 || cap.called {
			t.Errorf("%s %s: harus 400, dapat %d", c.method, c.path, rec.Code)
		}
	}
}
