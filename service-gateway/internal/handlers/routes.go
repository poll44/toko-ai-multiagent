// Package handlers: routing gateway, auth, dan health developer (TRD 01 §4, §6).
package handlers // nama package: handlers

import ( // package yang dipakai
	"context"       // context
	"crypto/subtle" // perbandingan constant-time
	"net"           // parsing alamat IP
	"net/http"      // tipe HTTP
	"net/url"       // parsing URL upstream
	"strings"       // utilitas string
	"sync"          // sinkronisasi goroutine
	"time"          // durasi

	"toko-ai-microservices/internal/shared/httputil"               // helper HTTP bersama
	"toko-ai-microservices/internal/shared/idguard"                // penegak identitas untuk agent
	"toko-ai-microservices/internal/shared/validate"               // validator
	"toko-ai-microservices/service-gateway/internal/auth"          // penerbitan JWT
	"toko-ai-microservices/service-gateway/internal/config"        // konfigurasi
	mw "toko-ai-microservices/service-gateway/internal/middleware" // middleware gateway (alias mw)
	"toko-ai-microservices/service-gateway/internal/proxy"         // reverse proxy
	"toko-ai-microservices/service-gateway/internal/store"         // akses data user
)

// AgentAppName: nama aplikasi ADK yang diizinkan (harus sama dengan root agent di service-agent).
const AgentAppName = "toko_orchestrator"

// Deps: dependensi handler gateway.
type Deps struct {
	Cfg   *config.Config // konfigurasi
	Users *store.Repo    // repository user
}

// Handler merakit seluruh gateway: route, middleware (urutan TRD 01 §7), dan fallback JSON 404.
func Handler(d *Deps) http.Handler {
	cfg := d.Cfg                            // pintasan konfigurasi
	authn := mw.Authenticate(cfg.JWTSecret) // middleware autentikasi JWT

	mux := http.NewServeMux() // router

	// --- Endpoint milik gateway ---
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { // health check
		httputil.Health("service-gateway")(w, r)
	})
	mux.Handle("/auth/login", mw.Method(http.MethodPost, http.HandlerFunc(d.login)))                                                       // login
	mux.Handle("/auth/demo-users", mw.Method(http.MethodGet, http.HandlerFunc(d.demoUsers)))                                               // daftar akun demo
	mux.Handle("/auth/me", mw.Method(http.MethodGet, authn(mw.RequireAuth(http.HandlerFunc(d.me)))))                                       // profil user login
	mux.Handle("/api/dev/health", mw.Method(http.MethodGet, authn(mw.RequireRoles(httputil.RoleAdminDev)(http.HandlerFunc(d.devHealth))))) // health semua service (ADMIN_DEV)

	// --- Reverse proxy (path publik == path upstream, tanpa rewrite) ---
	mk := func(raw string, timeout time.Duration, strip string, stripOrigin bool) http.Handler { // pembuat proxy
		u, err := url.Parse(raw) // parse URL upstream
		if err != nil {
			panic("URL upstream tidak valid: " + raw) // konfigurasi salah: gagal cepat
		}
		return proxy.New(proxy.Options{Target: u, InternalKey: cfg.InternalAPIKey, Timeout: timeout, StripPrefix: strip, StripBrowserOrigin: stripOrigin})
	}
	catalog := authn(mk(cfg.CatalogURL, cfg.UpstreamTimeout, "", false))                // semua role (GUEST boleh)
	orders := authn(mw.RequireAuth(mk(cfg.OrderURL, cfg.UpstreamTimeout, "", false)))   // GUEST => 401
	tickets := authn(mw.RequireAuth(mk(cfg.TicketURL, cfg.UpstreamTimeout, "", false))) // GUEST => 401
	knowledge := authn(mk(cfg.TicketURL, cfg.UpstreamTimeout, "", false))               // KB ada di service-ticket; GUEST boleh
	// Agent: buang prefiks /api/agent → idguard (allowlist rute + penegakan identitas) → proxy tanpa Origin.
	agentProxy := mk(cfg.AgentURL, cfg.AgentTimeout, "", true)
	agent := authn(http.StripPrefix("/api/agent", idguard.Enforce(AgentAppName, agentProxy)))

	for _, p := range []string{"/api/products", "/api/products/"} { // rute katalog
		mux.Handle(p, catalog)
	}
	for _, p := range []string{"/api/orders", "/api/orders/"} { // rute pesanan
		mux.Handle(p, orders)
	}
	for _, p := range []string{"/api/tickets", "/api/tickets/"} { // rute tiket
		mux.Handle(p, tickets)
	}
	mux.Handle("/api/knowledge/", knowledge) // rute knowledge base
	mux.Handle("/api/agent/", agent)         // rute agent AI

	// Fallback JSON untuk path tak dikenal (ServeMux default berupa teks polos).
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httputil.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Rute tidak ditemukan.", nil)
	})

	return httputil.Chain( // urutan middleware dari luar ke dalam
		httputil.Recover,           // tangkap panic
		httputil.RequestID,         // beri request id
		httputil.Logging,           // log request
		mw.CORS(cfg.CORSOrigin),    // CORS
		mw.SanitizeIdentityHeaders, // hapus header identitas palsu
		mw.BlockInternalPaths,      // blokir /internal
	)(mux)
}

// ---------- auth ----------

// login: POST /auth/login — verifikasi email & password, lalu terbitkan JWT.
func (d *Deps) login(w http.ResponseWriter, r *http.Request) {
	var b struct { // payload
		Email    string `json:"email"`    // email
		Password string `json:"password"` // password
	}
	if err := httputil.ReadJSON(w, r, &b); err != nil { // baca body
		httputil.Fail(w, r, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(b.Email)) // normalisasi email
	v := validate.New()
	if v.Required("email", email) { // email wajib
		v.Email("email", email) // dan harus valid
	}
	v.Len("password", b.Password, 1, 128) // password 1–128 karakter
	if e := v.Err(); e != nil {
		httputil.Fail(w, r, e)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr) // IP klien untuk audit
	fail := func(userID string) {               // jalan pintas menolak login
		_ = d.Users.LogLogin(r.Context(), userID, email, false, ip) // catat kegagalan
		// Pesan sama untuk email tak dikenal / password salah / akun nonaktif (tidak membocorkan alasan).
		httputil.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Email atau password salah.", nil)
	}
	cred, err := d.Users.FindByEmail(r.Context(), email) // cari user
	if err != nil {
		httputil.Fail(w, r, err)
		return
	}
	if cred == nil { // email tidak dikenal
		fail("")
		return
	}
	if !cred.Active || subtle.ConstantTimeCompare([]byte(cred.Password), []byte(b.Password)) != 1 { // nonaktif / password salah
		fail(cred.ID)
		return
	}
	token, err := auth.Sign(d.Cfg.JWTSecret, d.Cfg.JWTExpiresIn, cred.ID, cred.Email, cred.Name, cred.Role) // terbitkan JWT
	if err != nil {
		httputil.Fail(w, r, err)
		return
	}
	_ = d.Users.LogLogin(r.Context(), cred.ID, email, true, ip) // catat keberhasilan
	httputil.WriteData(w, http.StatusOK, map[string]any{
		"token": token, "token_type": "Bearer", "expires_in": int(d.Cfg.JWTExpiresIn.Seconds()), "user": cred.User,
	})
}

// demoUsers: GET /auth/demo-users — daftar akun demo untuk pemilih akun di frontend.
func (d *Deps) demoUsers(w http.ResponseWriter, r *http.Request) {
	users, err := d.Users.DemoUsers(r.Context())
	if err != nil {
		httputil.Fail(w, r, err)
		return
	}
	httputil.WriteData(w, http.StatusOK, users)
}

// me: dari klaim JWT (tanpa query DB).
func (d *Deps) me(w http.ResponseWriter, r *http.Request) {
	c := mw.ClaimsFrom(r.Context()) // klaim dari context
	if c == nil {                   // seharusnya tak terjadi setelah RequireAuth
		httputil.Fail(w, r, httputil.ErrUnauthenticated("Token tidak valid."))
		return
	}
	httputil.WriteData(w, http.StatusOK, store.User{ID: c.Subject, Email: c.Email, Name: c.Name, Role: c.Role})
}

// ---------- GET /api/dev/health ----------

// devHealth: memeriksa /health semua service secara paralel (timeout 3 detik) -> {nama: "up"|"down"}.
func (d *Deps) devHealth(w http.ResponseWriter, r *http.Request) {
	targets := map[string]string{ // daftar service yang dicek
		"catalog": d.Cfg.CatalogURL, "order": d.Cfg.OrderURL, "ticket": d.Cfg.TicketURL,
		"mcp": d.Cfg.MCPBaseURL, "agent": d.Cfg.AgentURL,
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second) // batas total 3 detik
	defer cancel()
	var mu sync.Mutex                 // melindungi map hasil
	var wg sync.WaitGroup             // menunggu semua goroutine
	result := map[string]string{}     // hasil nama -> status
	for name, base := range targets { // cek tiap service
		wg.Add(1)
		go func() { // paralel
			defer wg.Done()
			status := "down" // default: mati
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/health", nil)
			if err == nil {
				if resp, err := http.DefaultClient.Do(req); err == nil { // panggil /health
					if resp.StatusCode/100 == 2 { // respons 2xx
						status = "up"
					}
					resp.Body.Close()
				}
			}
			mu.Lock() // simpan hasil dengan aman
			result[name] = status
			mu.Unlock()
		}()
	}
	wg.Wait() // tunggu semua selesai
	httputil.WriteData(w, http.StatusOK, result)
}
