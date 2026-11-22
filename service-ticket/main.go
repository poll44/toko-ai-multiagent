// service-ticket (:4003) — tiket komplain + knowledge base kebijakan (RAG). Lihat docs/prd/04-SERVICE-TICKET.md
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
	"toko-ai-microservices/service-ticket/internal/config"
	"toko-ai-microservices/service-ticket/internal/handlers"
	"toko-ai-microservices/service-ticket/internal/knowledge"
	"toko-ai-microservices/service-ticket/internal/orderclient"
	"toko-ai-microservices/service-ticket/internal/service"
	"toko-ai-microservices/service-ticket/internal/store"
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
		slog.Error("gagal membuka ticket.db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	repo := store.NewRepo(db)
	if err := repo.SeedTickets(ctx); err != nil {
		slog.Error("seed tiket gagal", "err", err)
		os.Exit(1)
	}
	if err := knowledge.SeedDocs(ctx, repo); err != nil {
		slog.Error("seed knowledge base gagal", "err", err)
		os.Exit(1)
	}

	emb := embedding.New(cfg.GoogleAPIKey, cfg.EmbeddingModel, cfg.EmbeddingDim, cfg.EmbeddingTimeout)
	if !emb.Enabled() {
		slog.Warn("GOOGLE_API_KEY kosong: knowledge search akan memakai keyword_fallback")
	}
	ix := knowledge.NewIndexer(repo, emb)
	ix.Start(ctx, 60*time.Second) // indexing awal + worker retry tiap 60 detik (non-blocking)

	svc := service.New(repo, orderclient.New(cfg.OrderURL, cfg.InternalAPIKey, cfg.UpstreamTimeout))
	mux := http.NewServeMux()
	handlers.Register(mux, cfg.InternalAPIKey, &handlers.Deps{Svc: svc, Repo: repo, Index: ix, MinScore: cfg.SemanticMinScore})
	handler := httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)(mux)

	if err := httputil.RunServer(ctx, "service-ticket", ":"+cfg.Port, handler); err != nil {
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
