// service-agent (:8000) — Google ADK (Go): Orchestrator + 2 sub-agent. Lihat docs/trd/06-SERVICE-AGENT.md
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/server/adkrest"
	"google.golang.org/adk/session"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/idguard"
	"toko-ai-microservices/service-agent/internal/agents"
	"toko-ai-microservices/service-agent/internal/config"
)

// newHandler: server REST ADK di balik InternalAuth + Identity + idguard (allowlist rute & penegakan identitas).
func newHandler(cfg *config.Config, root agent.Agent) (http.Handler, error) {
	rest, err := adkrest.NewServer(adkrest.ServerConfig{
		AgentLoader:     agent.NewSingleLoader(root),
		SessionService:  session.InMemoryService(), // dev; ganti dengan penyimpanan persisten bila perlu
		SSEWriteTimeout: 120 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	secured := httputil.Chain(
		httputil.InternalAuthMiddleware(cfg.InternalAPIKey),
		httputil.IdentityMiddleware(),
	)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", httputil.Health("service-agent"))
	mux.Handle("/", secured(idguard.Enforce(config.AppName, rest)))
	return httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)(mux), nil
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("konfigurasi tidak valid", "err", err)
		os.Exit(1)
	}
	httputil.InitLogger(cfg.AppEnv)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root, err := agents.Build(ctx, cfg)
	if err != nil {
		slog.Error("gagal membangun agent", "err", err)
		os.Exit(1)
	}
	h, err := newHandler(cfg, root)
	if err != nil {
		slog.Error("gagal membuat server REST ADK", "err", err)
		os.Exit(1)
	}
	if err := httputil.RunServer(ctx, "service-agent", ":"+cfg.Port, h); err != nil {
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
