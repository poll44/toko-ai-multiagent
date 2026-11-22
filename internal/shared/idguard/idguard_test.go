package idguard_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/idguard"
)

const app = "toko_orchestrator"

type captured struct {
	called bool
	body   map[string]any
	method string
	path   string
}

func run(t *testing.T, id httputil.Identity, method, path, body string) (*httptest.ResponseRecorder, *captured) {
	t.Helper()
	cap := &captured{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.called, cap.method, cap.path = true, r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		if len(b) > 0 {
			_ = json.Unmarshal(b, &cap.body)
		}
		w.WriteHeader(200)
	})
	h := idguard.Enforce(app, next)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(httputil.WithIdentity(context.Background(), id))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, cap
}

var budi = httputil.Identity{UserID: "USR-01", Role: "CUSTOMER"}

func errCode(rec *httptest.ResponseRecorder) string {
	var m struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return m.Error.Code
}

func TestCreateSessionOverwritesIdentityState(t *testing.T) {
	for _, p := range []string{
		"/apps/toko_orchestrator/users/USR-01/sessions",
		"/apps/toko_orchestrator/users/USR-01/sessions/s-123",
	} {
		// klien mencoba menyisipkan identitas palsu + state kustom
		rec, c := run(t, budi, "POST", p, `{"state":{"user_id":"ADM-01","user_role":"ADMIN_SISTEM","theme":"dark"}}`)
		if rec.Code != 200 || !c.called {
			t.Fatalf("%s: status %d", p, rec.Code)
		}
		st := c.body["state"].(map[string]any)
		if st["user_id"] != "USR-01" || st["user_role"] != "CUSTOMER" {
			t.Fatalf("%s: identitas harus ditimpa dari header: %v", p, st)
		}
		if st["theme"] != "dark" {
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

func TestUserIDMismatchForbidden(t *testing.T) {
	cases := []struct{ method, path, body string }{
		{"POST", "/apps/toko_orchestrator/users/USR-02/sessions", `{}`},
		{"GET", "/apps/toko_orchestrator/users/USR-02/sessions/s-1", ""},
		{"DELETE", "/apps/toko_orchestrator/users/USR-02/sessions/s-1", ""},
		{"POST", "/run", `{"appName":"toko_orchestrator","userId":"USR-02","sessionId":"s","newMessage":{}}`},
		{"POST", "/run_sse", `{"appName":"toko_orchestrator","userId":"ADM-01","sessionId":"s","newMessage":{}}`},
		{"POST", "/run", `{"appName":"toko_orchestrator","sessionId":"s","newMessage":{}}`}, // userId hilang
	}
	for _, c := range cases {
		rec, cap := run(t, budi, c.method, c.path, c.body)
		if rec.Code != 403 || errCode(rec) != "FORBIDDEN" || cap.called {
			t.Errorf("%s %s: harus 403 FORBIDDEN tanpa menyentuh ADK, dapat %d %s called=%v", c.method, c.path, rec.Code, errCode(rec), cap.called)
		}
	}
}

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

func TestRouteAllowlist(t *testing.T) {
	for _, p := range []string{
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

func TestMalformedBodies(t *testing.T) {
	for _, c := range []struct{ method, path, body string }{
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
