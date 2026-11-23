// service-gateway (:8080) — autentikasi JWT, injeksi header identitas, reverse proxy. Lihat docs/trd/01-SERVICE-GATEWAY.md
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/service-gateway/internal/config"
	"toko-ai-microservices/service-gateway/internal/handlers"
	"toko-ai-microservices/service-gateway/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("konfigurasi tidak valid", "err", err)
		os.Exit(1)
	}
	httputil.InitLogger(cfg.AppEnv)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.AuthDBPath)
	if err != nil {
		slog.Error("gagal membuka auth.db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	h := handlers.Handler(&handlers.Deps{Cfg: cfg, Users: store.NewRepo(db)})
	if err := httputil.RunServer(ctx, "service-gateway", ":"+cfg.Port, h); err != nil {
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
