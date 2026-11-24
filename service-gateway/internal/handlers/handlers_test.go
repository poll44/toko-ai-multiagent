package handlers_test // test black-box untuk handler service-gateway

import ( // package yang dipakai
	"bufio"             // membaca baris SSE
	"context"           // context
	"encoding/base64"   // membuat token alg=none palsu
	"encoding/json"     // encode/decode JSON
	"io"                // membaca/menulis body
	"net/http"          // tipe HTTP
	"net/http/httptest" // server uji
	"path/filepath"     // path database sementara
	"strings"           // utilitas string
	"sync"              // mutex
	"testing"           // framework test
	"time"              // waktu

	"github.com/golang-jwt/jwt/v5" // library JWT (untuk membuat token uji)

	"toko-ai-microservices/service-gateway/internal/auth"     // penerbitan/verifikasi JWT
	"toko-ai-microservices/service-gateway/internal/config"   // konfigurasi
	"toko-ai-microservices/service-gateway/internal/handlers" // paket yang diuji
	"toko-ai-microservices/service-gateway/internal/store"    // repository user
)

// Konstanta uji.
const (
	secret      = "gateway-test-secret"   // secret JWT
	internalKey = "gateway-internal-key"  // internal API key
	origin      = "http://localhost:3000" // origin frontend yang diizinkan
)

// upstream: service domain palsu yang merekam request yang sampai.
type upstream struct {
	srv  *httptest.Server // server palsu
	mu   sync.Mutex       // melindungi reqs
	reqs []*captured      // request yang diterima
}

// captured: rekaman satu request yang diterima upstream.
type captured struct {
	Method, Path, RawQuery string      // metode, path, query
	Header                 http.Header // header
	Body                   string      // body
}

// newUpstream menjalankan upstream palsu; h (opsional) menentukan respons kustom.
func newUpstream(t *testing.T, h http.HandlerFunc) *upstream {
	u := &upstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.reqs = append(u.reqs, &captured{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), string(b)}) // rekam request
		u.mu.Unlock()
		if h != nil { // respons kustom
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json") // respons default
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	t.Cleanup(u.srv.Close)
	return u
}

// last mengembalikan request terakhir (nil bila belum ada).
func (u *upstream) last() *captured {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.reqs) == 0 {
		return nil
	}
	return u.reqs[len(u.reqs)-1]
}

// count mengembalikan jumlah request yang diterima.
func (u *upstream) count() int { u.mu.Lock(); defer u.mu.Unlock(); return len(u.reqs) }

// env: lingkungan uji (gateway + semua upstream palsu).
type env struct {
	srv                                  *httptest.Server // server gateway
	catalog, order, ticket, mcp, agentUp *upstream        // upstream palsu
	repo                                 *store.Repo      // repository user
}

// setup menyiapkan gateway uji. mod (opsional) mengubah konfigurasi; agentHandler (opsional) mengatur respons agent.
func setup(t *testing.T, mod func(*config.Config), agentHandler http.HandlerFunc) *env {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "auth.db")) // database sementara
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{
		catalog: newUpstream(t, nil), order: newUpstream(t, nil), ticket: newUpstream(t, nil),
		mcp: newUpstream(t, nil), agentUp: newUpstream(t, agentHandler), repo: store.NewRepo(db),
	}
	cfg := &config.Config{ // konfigurasi yang menunjuk ke upstream palsu
		Port: "0", JWTSecret: secret, JWTExpiresIn: time.Hour, InternalAPIKey: internalKey, CORSOrigin: origin,
		CatalogURL: e.catalog.srv.URL, OrderURL: e.order.srv.URL, TicketURL: e.ticket.srv.URL,
		MCPBaseURL: e.mcp.srv.URL, AgentURL: e.agentUp.srv.URL,
		UpstreamTimeout: 2 * time.Second, AgentTimeout: 5 * time.Second,
	}
	if mod != nil { // ubah konfigurasi sesuai kebutuhan test
		mod(cfg)
	}
	e.srv = httptest.NewServer(handlers.Handler(&handlers.Deps{Cfg: cfg, Users: e.repo})) // jalankan gateway
	t.Cleanup(e.srv.Close)
	return e
}

// resp: ringkasan respons HTTP.
type resp struct {
	Status int            // status code
	Body   map[string]any // body JSON
	Header http.Header    // header respons
}

// req mengirim request ke gateway dengan header tambahan hdr.
func (e *env) req(t *testing.T, method, path, body string, hdr map[string]string) resp {
	t.Helper()
	r, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr { // pasang header tambahan
		r.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return resp{res.StatusCode, m, res.Header}
}

// bearer membuat header Authorization: Bearer <tok>.
func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

// login login dengan email (password demo) lalu mengembalikan token JWT.
func (e *env) login(t *testing.T, email string) string {
	t.Helper()
	r := e.req(t, "POST", "/auth/login", `{"email":"`+email+`","password":"123456"}`, nil)
	if r.Status != 200 {
		t.Fatalf("login %s: %d %v", email, r.Status, r.Body)
	}
	return r.Body["data"].(map[string]any)["token"].(string)
}

// code mengambil kode error dari respons ("" bila bukan error).
func code(r resp) string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

// ───────────────────────── auth ─────────────────────────

// TestLoginAllDemoUsers: keempat akun demo bisa login dan token berisi klaim yang benar.
func TestLoginAllDemoUsers(t *testing.T) {
	e := setup(t, nil, nil)
	for email, want := range map[string][3]string{ // email => (id, role, nama)
		"budi@toko.com":  {"USR-01", "CUSTOMER", "Budi Santoso"},
		"siti@toko.com":  {"USR-02", "CUSTOMER", "Siti Rahma"},
		"admin@toko.com": {"ADM-01", "ADMIN_SISTEM", "Admin Sistem"},
		"dev@toko.com":   {"DEV-01", "ADMIN_DEV", "Developer Toko"},
	} {
		r := e.req(t, "POST", "/auth/login", `{"email":"`+strings.ToUpper(email)+`","password":"123456"}`, nil) // email di-lowercase
		if r.Status != 200 {
			t.Fatalf("%s: %d %v", email, r.Status, r.Body)
		}
		d := r.Body["data"].(map[string]any)
		u := d["user"].(map[string]any)
		if d["token_type"] != "Bearer" || d["expires_in"].(float64) != 3600 || u["id"] != want[0] || u["role"] != want[1] || u["name"] != want[2] {
			t.Fatalf("%s: %v", email, d)
		}
		if _, leak := u["password"]; leak { // password tidak boleh bocor
			t.Fatal("password tidak boleh bocor")
		}
		c, err := auth.Parse(secret, d["token"].(string)) // verifikasi token
		if err != nil || c.Subject != want[0] || c.Role != want[1] || c.Email != email {
			t.Fatalf("klaim JWT: %+v err=%v", c, err)
		}
	}
}

// TestLoginFailures: kegagalan login (kredensial salah, akun nonaktif, validasi, metode salah).
func TestLoginFailures(t *testing.T) {
	e := setup(t, nil, nil)
	for name, body := range map[string]string{
		"password salah": `{"email":"budi@toko.com","password":"salah"}`,
		"email tak ada":  `{"email":"nobody@toko.com","password":"123456"}`,
	} {
		r := e.req(t, "POST", "/auth/login", body, nil)
		if r.Status != 401 || code(r) != "INVALID_CREDENTIALS" {
			t.Errorf("%s: %d %s", name, r.Status, code(r))
		}
	}
	// pesan identik (tidak membocorkan apakah email terdaftar)
	m1 := e.req(t, "POST", "/auth/login", `{"email":"budi@toko.com","password":"x"}`, nil).Body["error"].(map[string]any)["message"]
	m2 := e.req(t, "POST", "/auth/login", `{"email":"nobody@toko.com","password":"x"}`, nil).Body["error"].(map[string]any)["message"]
	if m1 != m2 {
		t.Fatalf("pesan harus identik: %v vs %v", m1, m2)
	}
	// akun nonaktif
	if _, err := dbExec(t, e, `UPDATE users SET is_active=0 WHERE email='siti@toko.com'`); err != nil {
		t.Fatal(err)
	}
	if r := e.req(t, "POST", "/auth/login", `{"email":"siti@toko.com","password":"123456"}`, nil); r.Status != 401 {
		t.Fatalf("nonaktif: %d", r.Status)
	}
	for name, body := range map[string]string{
		"email invalid": `{"email":"bukan-email","password":"123456"}`,
		"pass kosong":   `{"email":"budi@toko.com","password":""}`,
		"body kosong":   ``,
		"json rusak":    `{`,
	} {
		if r := e.req(t, "POST", "/auth/login", body, nil); r.Status != 400 || code(r) != "VALIDATION_ERROR" {
			t.Errorf("%s: %d %s", name, r.Status, code(r))
		}
	}
	if r := e.req(t, "GET", "/auth/login", "", nil); r.Status != 405 || code(r) != "METHOD_NOT_ALLOWED" {
		t.Fatalf("GET login: %d %s", r.Status, code(r))
	}
}

// TestDemoUsers: daftar akun demo publik, terurut, dan tanpa password.
func TestDemoUsers(t *testing.T) {
	e := setup(t, nil, nil)
	r := e.req(t, "GET", "/auth/demo-users", "", nil)
	if r.Status != 200 {
		t.Fatal(r.Status)
	}
	var got []string
	for _, u := range r.Body["data"].([]any) {
		m := u.(map[string]any)
		if _, leak := m["password"]; leak {
			t.Fatal("password bocor")
		}
		got = append(got, m["id"].(string))
	}
	if strings.Join(got, ",") != "USR-01,USR-02,ADM-01,DEV-01" { // urutan role lalu id
		t.Fatalf("urutan: %v", got)
	}
}

// TestMeAndTokenRejection: /auth/me dan penolakan berbagai token tidak valid (tanpa diturunkan jadi GUEST).
func TestMeAndTokenRejection(t *testing.T) {
	e := setup(t, nil, nil)
	tok := e.login(t, "budi@toko.com")
	if r := e.req(t, "GET", "/auth/me", "", bearer(tok)); r.Status != 200 || r.Body["data"].(map[string]any)["id"] != "USR-01" {
		t.Fatalf("me: %d %v", r.Status, r.Body)
	}
	if r := e.req(t, "GET", "/auth/me", "", nil); r.Status != 401 || code(r) != "UNAUTHENTICATED" {
		t.Fatalf("me tanpa token: %d", r.Status)
	}

	claims := auth.Claims{Email: "budi@toko.com", Name: "Budi", Role: "CUSTOMER",
		RegisteredClaims: jwt.RegisteredClaims{Subject: "USR-01", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
	hs512, _ := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte(secret))                                      // token dengan algoritma lain
	expired, _ := auth.Sign(secret, -time.Minute, "USR-01", "budi@toko.com", "Budi", "CUSTOMER")                                    // token kedaluwarsa
	wrongKey, _ := auth.Sign("kunci-lain", time.Hour, "USR-01", "budi@toko.com", "Budi", "CUSTOMER")                                // secret salah
	b64 := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }                                         // encoder base64url
	algNone := b64(`{"alg":"none","typ":"JWT"}`) + "." + b64(`{"sub":"ADM-01","role":"ADMIN_SISTEM","exp":`+"9999999999"+`}`) + "." // token alg=none
	guestRole, _ := auth.Sign(secret, time.Hour, "USR-09", "x@x.com", "X", "GUEST")                                                 // role GUEST di token
	badRole, _ := auth.Sign(secret, time.Hour, "USR-09", "x@x.com", "X", "ROOT")                                                    // role tak dikenal
	tampered := tok[:len(tok)-3] + "AAA"                                                                                            // tanda tangan dimodifikasi

	for name, h := range map[string]map[string]string{
		"HS512 (algoritma lain)": bearer(hs512), "kedaluwarsa": bearer(expired), "secret salah": bearer(wrongKey),
		"alg=none": bearer(algNone), "role GUEST": bearer(guestRole), "role tak dikenal": bearer(badRole),
		"dimodifikasi": bearer(tampered), "sampah": bearer("bukan.jwt.sama-sekali"),
		"skema Basic": {"Authorization": "Basic " + tok}, "tanpa skema": {"Authorization": tok}, "Bearer kosong": {"Authorization": "Bearer "},
	} {
		if r := e.req(t, "GET", "/auth/me", "", h); r.Status != 401 || code(r) != "UNAUTHENTICATED" {
			t.Errorf("%s harus 401: %d %s", name, r.Status, code(r))
		}
		// token tidak valid pada route publik TIDAK boleh diturunkan menjadi GUEST
		if r := e.req(t, "GET", "/api/products", "", h); r.Status != 401 {
			t.Errorf("%s pada route publik harus 401: %d", name, r.Status)
		}
	}
	if e.catalog.count() != 0 { // tidak boleh ada yang sampai ke upstream
		t.Fatalf("request ber-token invalid tidak boleh sampai ke upstream: %d", e.catalog.count())
	}
}

// ───────────────────────── proxy & header ─────────────────────────

// TestProxyInjectsIdentityHeaders: gateway menyuntik header identitas asli dan membuang yang palsu.
func TestProxyInjectsIdentityHeaders(t *testing.T) {
	e := setup(t, nil, nil)

	// GUEST ke katalog (publik) + header palsu dari klien harus dibuang
	r := e.req(t, "GET", "/api/products?q=powerbank&max_price=400000", "", map[string]string{
		"x-user-id": "ADM-01", "x-user-role": "ADMIN_SISTEM", "x-internal-api-key": "tebak", "x-service-name": "service-order",
		"x-request-id": "bukan-uuid",
	})
	if r.Status != 200 {
		t.Fatalf("guest products: %d %v", r.Status, r.Body)
	}
	c := e.catalog.last()
	if c.Path != "/api/products" || c.RawQuery != "q=powerbank&max_price=400000" {
		t.Fatalf("path/query harus identik: %s?%s", c.Path, c.RawQuery)
	}
	for h, want := range map[string]string{"X-User-Id": "GUEST", "X-User-Role": "GUEST", "X-Internal-Api-Key": internalKey} {
		if got := c.Header.Get(h); got != want {
			t.Errorf("%s = %q, want %q", h, got, want)
		}
	}
	if c.Header.Get("X-Service-Name") != "" {
		t.Error("x-service-name palsu harus dihapus")
	}
	if id := c.Header.Get("X-Request-Id"); id == "" || id == "bukan-uuid" {
		t.Errorf("x-request-id harus UUID baru: %q", id)
	}

	// CUSTOMER ke order: identitas dari JWT, Authorization TIDAK diteruskan
	tok := e.login(t, "budi@toko.com")
	h := bearer(tok)
	h["x-user-role"] = "ADMIN_SISTEM" // spoof
	r = e.req(t, "GET", "/api/orders/ORD-003", "", h)
	if r.Status != 200 {
		t.Fatalf("orders: %d", r.Status)
	}
	o := e.order.last()
	if o.Path != "/api/orders/ORD-003" || o.Header.Get("X-User-Id") != "USR-01" || o.Header.Get("X-User-Role") != "CUSTOMER" {
		t.Fatalf("identitas ke order: %v %s", o.Header, o.Path)
	}
	if o.Header.Get("Authorization") != "" {
		t.Fatal("JWT tidak boleh diteruskan ke service domain")
	}
	// body POST diteruskan utuh
	e.req(t, "POST", "/api/orders", `{"items":[{"product_id":"PRD-003","quantity":1}]}`, bearer(tok))
	if o := e.order.last(); o.Method != "POST" || !strings.Contains(o.Body, `"PRD-003"`) {
		t.Fatalf("body: %+v", o)
	}
	// admin -> catalog mutasi tetap lewat (RBAC ditegakkan service)
	adm := e.login(t, "admin@toko.com")
	e.req(t, "POST", "/api/products", `{"name":"x"}`, bearer(adm))
	if c := e.catalog.last(); c.Method != "POST" || c.Header.Get("X-User-Role") != "ADMIN_SISTEM" || c.Header.Get("X-User-Id") != "ADM-01" {
		t.Fatalf("admin ke catalog: %+v", c)
	}
	// request id dari klien yang valid dipertahankan & dikembalikan
	rid := "7c9e6679-7425-40de-944b-e07fc1f90ae7"
	r = e.req(t, "GET", "/api/products", "", map[string]string{"x-request-id": rid})
	if r.Header.Get("x-request-id") != rid || e.catalog.last().Header.Get("X-Request-Id") != rid {
		t.Fatalf("request id: %s / %s", r.Header.Get("x-request-id"), e.catalog.last().Header.Get("X-Request-Id"))
	}
}

// TestRouteGuards: GUEST ditolak di route privat, knowledge publik, 404 JSON, dan health.
func TestRouteGuards(t *testing.T) {
	e := setup(t, nil, nil)
	// GUEST ditolak 401 di gateway, upstream tidak disentuh
	for _, p := range []string{"/api/orders", "/api/orders/ORD-001", "/api/tickets", "/api/tickets/TKT-001"} {
		if r := e.req(t, "GET", p, "", nil); r.Status != 401 || code(r) != "UNAUTHENTICATED" {
			t.Errorf("%s guest: %d %s", p, r.Status, code(r))
		}
	}
	if e.order.count() != 0 || e.ticket.count() != 0 {
		t.Fatal("upstream tidak boleh dipanggil untuk GUEST")
	}
	// knowledge publik -> service-ticket
	if r := e.req(t, "GET", "/api/knowledge/search?q=retur", "", nil); r.Status != 200 {
		t.Fatalf("knowledge guest: %d", r.Status)
	}
	if k := e.ticket.last(); k == nil || k.Path != "/api/knowledge/search" || k.Header.Get("X-User-Role") != "GUEST" {
		t.Fatalf("knowledge upstream: %+v", k)
	}
	// route tak dikenal -> JSON 404 ; method salah pada /health tetap sehat
	if r := e.req(t, "GET", "/tidak/ada", "", nil); r.Status != 404 || code(r) != "NOT_FOUND" {
		t.Fatalf("404 json: %d %s", r.Status, code(r))
	}
	if r := e.req(t, "GET", "/health", "", nil); r.Status != 200 || r.Body["data"].(map[string]any)["service"] != "service-gateway" {
		t.Fatalf("health: %d %v", r.Status, r.Body)
	}
}

// TestInternalPathsBlocked: semua path yang memuat segmen "internal" (termasuk ter-encode) diblokir 404.
func TestInternalPathsBlocked(t *testing.T) {
	e := setup(t, nil, nil)
	tok := bearer(e.login(t, "budi@toko.com"))
	for _, p := range []string{
		"/internal/stock/decrement", "/api/products/internal/stock/decrement", "/api/orders/internal/x",
		"/api/products/%69nternal/x", "/api/products/%2e%2e/internal/stock", "/api/products/%252e%252e/internal",
		"/api/products/INTERNAL/x", "/api/tickets/..%2finternal",
	} {
		r := e.req(t, "POST", p, `{}`, tok)
		if r.Status != 404 {
			t.Errorf("%s harus 404, dapat %d", p, r.Status)
		}
	}
	if e.catalog.count()+e.order.count()+e.ticket.count() != 0 {
		t.Fatal("path /internal tidak boleh sampai ke upstream")
	}
}

// TestCORS: hanya origin frontend yang diizinkan; preflight dijawab gateway tanpa menyentuh upstream.
func TestCORS(t *testing.T) {
	e := setup(t, nil, nil)
	// preflight dari origin frontend
	pf := e.req(t, "OPTIONS", "/api/orders", "", map[string]string{"Origin": origin, "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization,content-type"})
	if pf.Status != 204 || pf.Header.Get("Access-Control-Allow-Origin") != origin ||
		!strings.Contains(pf.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("preflight: %d %v", pf.Status, pf.Header)
	}
	if e.order.count() != 0 {
		t.Fatal("preflight tidak boleh diteruskan ke upstream")
	}
	// respons biasa memuat ACAO untuk origin yang diizinkan
	r := e.req(t, "GET", "/api/products", "", map[string]string{"Origin": origin})
	if r.Header.Get("Access-Control-Allow-Origin") != origin || !strings.Contains(r.Header.Get("Access-Control-Expose-Headers"), "x-request-id") {
		t.Fatalf("ACAO: %v", r.Header)
	}
	// origin lain tidak mendapat izin
	r = e.req(t, "GET", "/api/products", "", map[string]string{"Origin": "http://evil.example"})
	if r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("origin asing tidak boleh diizinkan: %v", r.Header)
	}
	pf = e.req(t, "OPTIONS", "/api/orders", "", map[string]string{"Origin": "http://evil.example", "Access-Control-Request-Method": "POST"})
	if pf.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("preflight origin asing")
	}
}

// TestUpstreamErrors: upstream mati => 502, lambat => 504, dan error upstream diteruskan apa adanya.
func TestUpstreamErrors(t *testing.T) {
	// upstream mati => 502
	e := setup(t, func(c *config.Config) { c.CatalogURL = "http://127.0.0.1:1" }, nil)
	if r := e.req(t, "GET", "/api/products", "", nil); r.Status != 502 || code(r) != "UPSTREAM_ERROR" {
		t.Fatalf("mati: %d %s", r.Status, code(r))
	}
	// upstream lambat => 504
	slow := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(1500 * time.Millisecond) })
	e = setup(t, func(c *config.Config) { c.CatalogURL = slow.srv.URL; c.UpstreamTimeout = 200 * time.Millisecond }, nil)
	r := e.req(t, "GET", "/api/products", "", nil)
	if r.Status != 504 || code(r) != "UPSTREAM_TIMEOUT" {
		t.Fatalf("lambat: %d %s", r.Status, code(r))
	}
	if r.Body["error"].(map[string]any)["request_id"] == "" {
		t.Fatal("request_id harus ada pada error")
	}
	// error upstream diteruskan apa adanya
	bad := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":{"code":"INSUFFICIENT_STOCK","message":"x","details":null,"request_id":"r"}}`))
	})
	e = setup(t, func(c *config.Config) { c.CatalogURL = bad.srv.URL }, nil)
	if r := e.req(t, "GET", "/api/products", "", nil); r.Status != 409 || code(r) != "INSUFFICIENT_STOCK" {
		t.Fatalf("passthrough: %d %s", r.Status, code(r))
	}
}

// TestDevHealth: /api/dev/health hanya untuk ADMIN_DEV dan melaporkan status tiap service.
func TestDevHealth(t *testing.T) {
	e := setup(t, nil, nil)
	// upstream /health: semua "up" kecuali ticket yang dimatikan
	e.ticket.srv.Close()
	if r := e.req(t, "GET", "/api/dev/health", "", nil); r.Status != 401 {
		t.Fatalf("guest: %d", r.Status)
	}
	for _, email := range []string{"budi@toko.com", "admin@toko.com"} { // role lain => 403
		if r := e.req(t, "GET", "/api/dev/health", "", bearer(e.login(t, email))); r.Status != 403 || code(r) != "FORBIDDEN" {
			t.Fatalf("%s: %d %s", email, r.Status, code(r))
		}
	}
	r := e.req(t, "GET", "/api/dev/health", "", bearer(e.login(t, "dev@toko.com")))
	if r.Status != 200 {
		t.Fatalf("dev: %d %v", r.Status, r.Body)
	}
	d := r.Body["data"].(map[string]any)
	if d["catalog"] != "up" || d["order"] != "up" || d["mcp"] != "up" || d["agent"] != "up" || d["ticket"] != "down" {
		t.Fatalf("health: %v", d)
	}
}

// ───────────────────────── agent ─────────────────────────

// TestAgentProxy: proxy agent (buang prefiks, hapus Origin, timpa state identitas, allowlist rute, GUEST).
func TestAgentProxy(t *testing.T) {
	e := setup(t, nil, nil)
	tok := bearer(e.login(t, "budi@toko.com"))
	withOrigin := func(h map[string]string) map[string]string { // pembantu menambah Origin & Referer browser
		h["Origin"] = origin
		h["Referer"] = origin + "/"
		return h
	}

	// create session: prefiks /api/agent dibuang, Origin dihapus, state identitas DITIMPA
	r := e.req(t, "POST", "/api/agent/apps/toko_orchestrator/users/USR-01/sessions/s-1", `{"state":{"user_role":"ADMIN_SISTEM","x":1}}`, withOrigin(tok))
	if r.Status != 200 {
		t.Fatalf("create session: %d %v", r.Status, r.Body)
	}
	a := e.agentUp.last()
	if a.Path != "/apps/toko_orchestrator/users/USR-01/sessions/s-1" {
		t.Fatalf("path upstream: %s", a.Path)
	}
	if a.Header.Get("Origin") != "" || a.Header.Get("Referer") != "" {
		t.Fatalf("Origin/Referer harus dihapus: %v", a.Header)
	}
	var body struct{ State map[string]any }
	_ = json.Unmarshal([]byte(a.Body), &body)
	if body.State["user_id"] != "USR-01" || body.State["user_role"] != "CUSTOMER" || body.State["x"].(float64) != 1 {
		t.Fatalf("state: %v", body.State)
	}
	if a.Header.Get("X-User-Id") != "USR-01" || a.Header.Get("Authorization") != "" || a.Header.Get("X-Internal-Api-Key") != internalKey {
		t.Fatalf("header ke agent: %v", a.Header)
	}

	// /run: userId sesuai => lolos
	run := `{"appName":"toko_orchestrator","userId":"USR-01","sessionId":"s-1","newMessage":{"role":"user","parts":[{"text":"hai"}]}}`
	if r := e.req(t, "POST", "/api/agent/run", run, withOrigin(tok)); r.Status != 200 || e.agentUp.last().Path != "/run" {
		t.Fatalf("run: %d %s", r.Status, e.agentUp.last().Path)
	}

	n := e.agentUp.count()        // jumlah request ke agent sebelum uji penolakan
	deny := map[string][3]string{ // nama => method, path, body
		"userId orang lain":        {"POST", "/api/agent/run", strings.Replace(run, "USR-01", "USR-02", 1)},
		"session user lain":        {"POST", "/api/agent/apps/toko_orchestrator/users/USR-02/sessions/s-9", `{}`},
		"stateDelta identitas":     {"POST", "/api/agent/run", `{"appName":"toko_orchestrator","userId":"USR-01","sessionId":"s-1","newMessage":{"role":"user","parts":[{"text":"hai"}]},"stateDelta":{"user_role":"ADMIN_SISTEM"}}`},
		"PATCH state identitas":    {"PATCH", "/api/agent/apps/toko_orchestrator/users/USR-01/sessions/s-1", `{"stateDelta":{"user_id":"ADM-01"}}`},
		"GET session user lain":    {"GET", "/api/agent/apps/toko_orchestrator/users/ADM-01/sessions/s-1", ``},
		"DELETE session user lain": {"DELETE", "/api/agent/apps/toko_orchestrator/users/USR-02/sessions/s-1", ``},
	}
	for name, d := range deny { // semua harus ditolak 403
		if r := e.req(t, d[0], d[1], d[2], tok); r.Status != 403 {
			t.Errorf("%s harus 403: %d", name, r.Status)
		}
	}
	for _, p := range []string{"/api/agent/list-apps", "/api/agent/debug/trace/x", "/api/agent/docs", "/api/agent/openapi.json", "/api/agent/run_live", "/api/agent/dev-ui/", "/api/agent/apps/toko_orchestrator/eval_sets"} { // rute di luar allowlist => 404
		if r := e.req(t, "GET", p, "", tok); r.Status != 404 {
			t.Errorf("%s harus 404: %d", p, r.Status)
		}
	}
	if e.agentUp.count() != n { // permintaan yang ditolak tidak boleh sampai ke agent
		t.Fatalf("permintaan yang ditolak tidak boleh sampai ke agent (%d -> %d)", n, e.agentUp.count())
	}

	// GUEST boleh memakai agent dengan userId=GUEST; identitas GUEST diteruskan
	guestRun := strings.Replace(run, "USR-01", "GUEST", 1)
	if r := e.req(t, "POST", "/api/agent/run", guestRun, nil); r.Status != 200 {
		t.Fatalf("guest run: %d", r.Status)
	}
	if g := e.agentUp.last(); g.Header.Get("X-User-Id") != "GUEST" || g.Header.Get("X-User-Role") != "GUEST" {
		t.Fatalf("identitas guest: %v", g.Header)
	}
	if r := e.req(t, "POST", "/api/agent/run", run, nil); r.Status != 403 { // GUEST mengaku USR-01
		t.Fatalf("guest menyamar: %d", r.Status)
	}
}

// TestAgentSSEIsFlushedImmediately: event SSE harus diteruskan seketika (tanpa buffering) oleh proxy.
func TestAgentSSEIsFlushedImmediately(t *testing.T) {
	release := make(chan struct{})                                    // sinyal agar upstream mengirim event kedua
	e := setup(t, nil, func(w http.ResponseWriter, r *http.Request) { // agent palsu yang mengalirkan SSE
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: pertama\n\n")
		w.(http.Flusher).Flush() // kirim event pertama segera
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
		_, _ = io.WriteString(w, "data: kedua\n\n")
	})
	run := `{"appName":"toko_orchestrator","userId":"GUEST","sessionId":"s-1","newMessage":{"role":"user","parts":[{"text":"hai"}]},"streaming":true}`
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/agent/run_sse", strings.NewReader(run))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	got := make(chan string, 1)
	go func() { // baca baris pertama di goroutine
		line, _ := bufio.NewReader(res.Body).ReadString('\n')
		got <- line
	}()
	select {
	case line := <-got: // harus tiba sebelum upstream selesai
		if !strings.Contains(line, "pertama") {
			t.Fatalf("baris pertama: %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event pertama tidak di-flush sebelum upstream selesai (buffering terdeteksi)")
	}
	close(release) // izinkan upstream menyelesaikan stream
}

// dbExec menjalankan SQL langsung pada auth.db (untuk menyiapkan kondisi uji).
func dbExec(t *testing.T, e *env, q string) (int64, error) {
	t.Helper()
	return e.repo.ExecForTest(context.Background(), q)
}
