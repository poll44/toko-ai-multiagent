// service-agent (:8000) — Google ADK (Go): Orchestrator + 2 sub-agent. Lihat docs/trd/06-SERVICE-AGENT.md
package main // titik masuk program service-agent

import ( // package yang dipakai
	"context"   // context & pembatalan
	"log/slog"  // logging terstruktur
	"net/http"  // server HTTP
	"os"        // exit & sinyal
	"os/signal" // menangkap sinyal OS
	"syscall"   // konstanta sinyal (SIGTERM)
	"time"      // durasi

	"google.golang.org/adk/agent"          // tipe agent ADK
	"google.golang.org/adk/server/adkrest" // server REST bawaan ADK
	"google.golang.org/adk/session"        // layanan sesi ADK

	"toko-ai-microservices/internal/shared/httputil"      // helper HTTP bersama
	"toko-ai-microservices/internal/shared/idguard"       // penegak identitas di depan ADK
	"toko-ai-microservices/service-agent/internal/agents" // perakitan agent
	"toko-ai-microservices/service-agent/internal/config" // konfigurasi service
)

// newHandler: server REST ADK di balik InternalAuth + Identity + idguard (allowlist rute & penegakan identitas).
func newHandler(cfg *config.Config, root agent.Agent) (http.Handler, error) {
	rest, err := adkrest.NewServer(adkrest.ServerConfig{ // buat server REST ADK
		AgentLoader:     agent.NewSingleLoader(root), // satu agent root
		SessionService:  session.InMemoryService(),   // dev; ganti dengan penyimpanan persisten bila perlu
		SSEWriteTimeout: 120 * time.Second,           // batas waktu tulis stream SSE
	})
	if err != nil {
		return nil, err
	}
	secured := httputil.Chain( // rantai middleware keamanan
		httputil.InternalAuthMiddleware(cfg.InternalAPIKey), // wajib kunci internal
		httputil.IdentityMiddleware(),                       // wajib identitas valid
	)
	mux := http.NewServeMux()                                                               // router
	mux.HandleFunc("GET /health", httputil.Health("service-agent"))                         // health check tanpa auth
	mux.Handle("/", secured(idguard.Enforce(config.AppName, rest)))                         // semua rute lain: aman + allowlist
	return httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)(mux), nil // middleware global
}

// main: memuat config, merakit agent, lalu menjalankan server sampai ada sinyal berhenti.
func main() {
	cfg, err := config.Load() // baca konfigurasi dari env
	if err != nil {
		slog.Error("konfigurasi tidak valid", "err", err)
		os.Exit(1) // gagal start
	}
	httputil.InitLogger(cfg.AppEnv)                                                        // pasang logger
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) // context batal saat Ctrl+C/SIGTERM
	defer stop()                                                                           // lepas listener sinyal

	root, err := agents.Build(ctx, cfg) // rakit Orchestrator + sub-agent
	if err != nil {
		slog.Error("gagal membangun agent", "err", err)
		os.Exit(1)
	}
	h, err := newHandler(cfg, root) // bangun handler HTTP
	if err != nil {
		slog.Error("gagal membuat server REST ADK", "err", err)
		os.Exit(1)
	}
	if err := httputil.RunServer(ctx, "service-agent", ":"+cfg.Port, h); err != nil { // jalankan server
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
