// service-order (:4002) — pesanan, simulasi pembayaran, stok via catalog. Lihat docs/trd/03-SERVICE-ORDER.md
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/service-order/internal/catalogclient"
	"toko-ai-microservices/service-order/internal/config"
	"toko-ai-microservices/service-order/internal/handlers"
	"toko-ai-microservices/service-order/internal/jobs"
	"toko-ai-microservices/service-order/internal/service"
	"toko-ai-microservices/service-order/internal/store"
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

	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		slog.Error("gagal membuka order.db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	repo := store.NewRepo(db)
	if err := repo.Seed(ctx); err != nil {
		slog.Error("seed order gagal", "err", err)
		os.Exit(1)
	}

	svc := service.New(repo, catalogclient.New(cfg.CatalogURL, cfg.InternalAPIKey, cfg.UpstreamTimeout), cfg.PaymentTTL)
	go jobs.ExpirePayments(ctx, svc, 60*time.Second) // berhenti saat ctx batal (graceful shutdown)

	mux := http.NewServeMux()
	handlers.Register(mux, cfg.InternalAPIKey, &handlers.Deps{Svc: svc})
	handler := httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)(mux)

	if err := httputil.RunServer(ctx, "service-order", ":"+cfg.Port, handler); err != nil {
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
