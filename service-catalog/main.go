// service-catalog (:4001) — produk, stok, pencarian semantik. Lihat docs/trd/02-SERVICE-CATALOG.md
package main // titik masuk program service-catalog

import ( // package yang dipakai
	"context"   // context & pembatalan
	"log/slog"  // logging terstruktur
	"net/http"  // server HTTP
	"os"        // exit
	"os/signal" // menangkap sinyal OS
	"syscall"   // konstanta SIGTERM
	"time"      // durasi

	"toko-ai-microservices/internal/shared/embedding"         // klien embedding Gemini
	"toko-ai-microservices/internal/shared/httputil"          // helper HTTP bersama
	"toko-ai-microservices/service-catalog/internal/config"   // konfigurasi
	"toko-ai-microservices/service-catalog/internal/handlers" // handler HTTP
	"toko-ai-microservices/service-catalog/internal/semantic" // indexer pencarian semantik
	"toko-ai-microservices/service-catalog/internal/store"    // akses database
)

// main: menyiapkan config, database, seed, indexer semantik, lalu menjalankan server HTTP.
func main() {
	cfg, err := config.Load() // baca konfigurasi
	if err != nil {
		slog.Error("konfigurasi tidak valid", "err", err)
		os.Exit(1) // gagal start
	}
	httputil.InitLogger(cfg.AppEnv) // pasang logger

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) // batal saat Ctrl+C/SIGTERM
	defer stop()                                                                           // lepas listener sinyal

	db, err := store.Open(ctx, cfg.DBPath) // buka & migrasi catalog.db
	if err != nil {
		slog.Error("gagal membuka catalog.db", "err", err)
		os.Exit(1)
	}
	defer db.Close() // tutup database saat keluar

	repo := store.NewRepo(db)              // repository produk
	if err := repo.Seed(ctx); err != nil { // isi 14 produk awal bila kosong
		slog.Error("seed produk gagal", "err", err)
		os.Exit(1)
	}

	emb := embedding.New(cfg.GoogleAPIKey, cfg.EmbeddingModel, cfg.EmbeddingDim, cfg.EmbeddingTimeout) // klien embedding
	if !emb.Enabled() {                                                                                // tanpa API key
		slog.Warn("GOOGLE_API_KEY kosong: semantic-search akan memakai keyword_fallback")
	}
	ix := semantic.New(repo, emb) // indexer semantik
	ix.Start(ctx, 60*time.Second) // indexing awal + worker retry tiap 60 detik (non-blocking)

	mux := http.NewServeMux()                                                                                         // router
	handlers.Register(mux, cfg.InternalAPIKey, &handlers.Deps{Repo: repo, Index: ix, MinScore: cfg.SemanticMinScore}) // daftarkan rute
	handler := httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)(mux)                            // middleware global

	if err := httputil.RunServer(ctx, "service-catalog", ":"+cfg.Port, handler); err != nil { // jalankan server
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
