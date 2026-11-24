// service-ticket (:4003) — tiket komplain + knowledge base kebijakan (RAG). Lihat docs/trd/04-SERVICE-TICKET.md
package main // titik masuk program service-ticket

import ( // package yang dipakai
	"context"   // context & pembatalan
	"log/slog"  // logging terstruktur
	"net/http"  // server HTTP
	"os"        // exit
	"os/signal" // menangkap sinyal OS
	"syscall"   // konstanta SIGTERM
	"time"      // durasi

	"toko-ai-microservices/internal/shared/embedding"           // klien embedding Gemini
	"toko-ai-microservices/internal/shared/httputil"            // helper HTTP bersama
	"toko-ai-microservices/service-ticket/internal/config"      // konfigurasi
	"toko-ai-microservices/service-ticket/internal/handlers"    // handler HTTP
	"toko-ai-microservices/service-ticket/internal/knowledge"   // knowledge base (RAG)
	"toko-ai-microservices/service-ticket/internal/orderclient" // klien ke service-order
	"toko-ai-microservices/service-ticket/internal/service"     // logika bisnis tiket
	"toko-ai-microservices/service-ticket/internal/store"       // akses database
)

// main: menyiapkan config, database, seed tiket & KB, indexer, service, lalu menjalankan server HTTP.
func main() {
	cfg, err := config.Load() // baca konfigurasi
	if err != nil {
		slog.Error("konfigurasi tidak valid", "err", err)
		os.Exit(1) // gagal start
	}
	httputil.InitLogger(cfg.AppEnv) // pasang logger

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) // batal saat Ctrl+C/SIGTERM
	defer stop()                                                                           // lepas listener sinyal

	db, err := store.Open(ctx, cfg.DBPath) // buka & migrasi ticket.db
	if err != nil {
		slog.Error("gagal membuka ticket.db", "err", err)
		os.Exit(1)
	}
	defer db.Close() // tutup database saat keluar

	repo := store.NewRepo(db)                     // repository tiket & KB
	if err := repo.SeedTickets(ctx); err != nil { // isi 5 tiket contoh bila kosong
		slog.Error("seed tiket gagal", "err", err)
		os.Exit(1)
	}
	if err := knowledge.SeedDocs(ctx, repo); err != nil { // isi 8 dokumen kebijakan bila kosong
		slog.Error("seed knowledge base gagal", "err", err)
		os.Exit(1)
	}

	emb := embedding.New(cfg.GoogleAPIKey, cfg.EmbeddingModel, cfg.EmbeddingDim, cfg.EmbeddingTimeout) // klien embedding
	if !emb.Enabled() {                                                                                // tanpa API key
		slog.Warn("GOOGLE_API_KEY kosong: knowledge search akan memakai keyword_fallback")
	}
	ix := knowledge.NewIndexer(repo, emb) // indexer knowledge base
	ix.Start(ctx, 60*time.Second)         // indexing awal + worker retry tiap 60 detik (non-blocking)

	svc := service.New(repo, orderclient.New(cfg.OrderURL, cfg.InternalAPIKey, cfg.UpstreamTimeout))                            // logika bisnis tiket
	mux := http.NewServeMux()                                                                                                   // router
	handlers.Register(mux, cfg.InternalAPIKey, &handlers.Deps{Svc: svc, Repo: repo, Index: ix, MinScore: cfg.SemanticMinScore}) // daftarkan rute
	handler := httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)(mux)                                      // middleware global

	if err := httputil.RunServer(ctx, "service-ticket", ":"+cfg.Port, handler); err != nil { // jalankan server
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
