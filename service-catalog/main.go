// service-catalog (:4001) — produk, stok, pencarian semantik. Lihat docs/prd/02-SERVICE-CATALOG.md
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"toko-ai-microservices/internal/shared/embedding"
	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/service-catalog/internal/config"
	"toko-ai-microservices/service-catalog/internal/handlers"
	"toko-ai-microservices/service-catalog/internal/semantic"
	"toko-ai-microservices/service-catalog/internal/store"
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
		slog.Error("gagal membuka catalog.db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	repo := store.NewRepo(db)
	if err := repo.Seed(ctx); err != nil {
		slog.Error("seed produk gagal", "err", err)
		os.Exit(1)
	}

	emb := embedding.New(cfg.GoogleAPIKey, cfg.EmbeddingModel, cfg.EmbeddingDim, cfg.EmbeddingTimeout)
	if !emb.Enabled() {
		slog.Warn("GOOGLE_API_KEY kosong: semantic-search akan memakai keyword_fallback")
	}
	ix := semantic.New(repo, emb)
	ix.Start(ctx, 60*time.Second) // indexing awal + worker retry tiap 60 detik (non-blocking)

	mux := http.NewServeMux()
	handlers.Register(mux, cfg.InternalAPIKey, &handlers.Deps{Repo: repo, Index: ix, MinScore: cfg.SemanticMinScore})
	handler := httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)(mux)

	if err := httputil.RunServer(ctx, "service-catalog", ":"+cfg.Port, handler); err != nil {
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
