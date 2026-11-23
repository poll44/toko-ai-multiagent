// Package handlers: routing gateway, auth, dan health developer (TRD 01 §4, §6).
package handlers

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/idguard"
	"toko-ai-microservices/internal/shared/validate"
	"toko-ai-microservices/service-gateway/internal/auth"
	"toko-ai-microservices/service-gateway/internal/config"
	mw "toko-ai-microservices/service-gateway/internal/middleware"
	"toko-ai-microservices/service-gateway/internal/proxy"
	"toko-ai-microservices/service-gateway/internal/store"
)

// AgentAppName: nama aplikasi ADK yang diizinkan (harus sama dengan root agent di service-agent).
const AgentAppName = "toko_orchestrator"

type Deps struct {
	Cfg   *config.Config
	Users *store.Repo
}

// Handler merakit seluruh gateway: route, middleware (urutan TRD 01 §7), dan fallback JSON 404.
func Handler(d *Deps) http.Handler {
	cfg := d.Cfg
	authn := mw.Authenticate(cfg.JWTSecret)

	mux := http.NewServeMux()

	// --- Endpoint milik gateway ---
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		httputil.Health("service-gateway")(w, r)
	})
	mux.Handle("/auth/login", mw.Method(http.MethodPost, http.HandlerFunc(d.login)))
	mux.Handle("/auth/demo-users", mw.Method(http.MethodGet, http.HandlerFunc(d.demoUsers)))
	mux.Handle("/auth/me", mw.Method(http.MethodGet, authn(mw.RequireAuth(http.HandlerFunc(d.me)))))
	mux.Handle("/api/dev/health", mw.Method(http.MethodGet, authn(mw.RequireRoles(httputil.RoleAdminDev)(http.HandlerFunc(d.devHealth)))))

	// --- Reverse proxy (path publik == path upstream, tanpa rewrite) ---
	mk := func(raw string, timeout time.Duration, strip string, stripOrigin bool) http.Handler {
		u, err := url.Parse(raw)
		if err != nil {
			panic("URL upstream tidak valid: " + raw)
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

	for _, p := range []string{"/api/products", "/api/products/"} {
		mux.Handle(p, catalog)
	}
	for _, p := range []string{"/api/orders", "/api/orders/"} {
		mux.Handle(p, orders)
	}
	for _, p := range []string{"/api/tickets", "/api/tickets/"} {
		mux.Handle(p, tickets)
	}
	mux.Handle("/api/knowledge/", knowledge)
	mux.Handle("/api/agent/", agent)

	// Fallback JSON untuk path tak dikenal (ServeMux default berupa teks polos).
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httputil.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "Rute tidak ditemukan.", nil)
	})

	return httputil.Chain(
		httputil.Recover,
		httputil.RequestID,
		httputil.Logging,
		mw.CORS(cfg.CORSOrigin),
		mw.SanitizeIdentityHeaders,
		mw.BlockInternalPaths,
	)(mux)
}

// ---------- auth ----------

func (d *Deps) login(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httputil.ReadJSON(w, r, &b); err != nil {
		httputil.Fail(w, r, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(b.Email))
	v := validate.New()
	if v.Required("email", email) {
		v.Email("email", email)
	}
	v.Len("password", b.Password, 1, 128)
	if e := v.Err(); e != nil {
		httputil.Fail(w, r, e)
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	fail := func(userID string) {
		_ = d.Users.LogLogin(r.Context(), userID, email, false, ip)
		// Pesan sama untuk email tak dikenal / password salah / akun nonaktif (tidak membocorkan alasan).
		httputil.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Email atau password salah.", nil)
	}
	cred, err := d.Users.FindByEmail(r.Context(), email)
	if err != nil {
		httputil.Fail(w, r, err)
		return
	}
	if cred == nil {
		fail("")
		return
	}
	if !cred.Active || subtle.ConstantTimeCompare([]byte(cred.Password), []byte(b.Password)) != 1 {
		fail(cred.ID)
		return
	}
	token, err := auth.Sign(d.Cfg.JWTSecret, d.Cfg.JWTExpiresIn, cred.ID, cred.Email, cred.Name, cred.Role)
	if err != nil {
		httputil.Fail(w, r, err)
		return
	}
	_ = d.Users.LogLogin(r.Context(), cred.ID, email, true, ip)
	httputil.WriteData(w, http.StatusOK, map[string]any{
		"token": token, "token_type": "Bearer", "expires_in": int(d.Cfg.JWTExpiresIn.Seconds()), "user": cred.User,
	})
}

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
	c := mw.ClaimsFrom(r.Context())
	if c == nil {
		httputil.Fail(w, r, httputil.ErrUnauthenticated("Token tidak valid."))
		return
	}
	httputil.WriteData(w, http.StatusOK, store.User{ID: c.Subject, Email: c.Email, Name: c.Name, Role: c.Role})
}

// ---------- GET /api/dev/health ----------

func (d *Deps) devHealth(w http.ResponseWriter, r *http.Request) {
	targets := map[string]string{
		"catalog": d.Cfg.CatalogURL, "order": d.Cfg.OrderURL, "ticket": d.Cfg.TicketURL,
		"mcp": d.Cfg.MCPBaseURL, "agent": d.Cfg.AgentURL,
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	result := map[string]string{}
	for name, base := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status := "down"
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/health", nil)
			if err == nil {
				if resp, err := http.DefaultClient.Do(req); err == nil {
					if resp.StatusCode/100 == 2 {
						status = "up"
					}
					resp.Body.Close()
				}
			}
			mu.Lock()
			result[name] = status
			mu.Unlock()
		}()
	}
	wg.Wait()
	httputil.WriteData(w, http.StatusOK, result)
}
