// service-order (:4002) — pesanan, simulasi pembayaran, stok via catalog. Lihat docs/trd/03-SERVICE-ORDER.md
package main // titik masuk program service-order

import ( // package yang dipakai
	"context"   // context & pembatalan
	"log/slog"  // logging terstruktur
	"net/http"  // server HTTP
	"os"        // exit
	"os/signal" // menangkap sinyal OS
	"syscall"   // konstanta SIGTERM
	"time"      // durasi

	"toko-ai-microservices/internal/shared/httputil"             // helper HTTP bersama
	"toko-ai-microservices/service-order/internal/catalogclient" // klien ke service-catalog
	"toko-ai-microservices/service-order/internal/config"        // konfigurasi
	"toko-ai-microservices/service-order/internal/handlers"      // handler HTTP
	"toko-ai-microservices/service-order/internal/jobs"          // job latar belakang
	"toko-ai-microservices/service-order/internal/service"       // logika bisnis
	"toko-ai-microservices/service-order/internal/store"         // akses database
)

// main: menyiapkan config, database, seed, service, job kedaluwarsa, lalu menjalankan server HTTP.
func main() {
	cfg, err := config.Load() // baca konfigurasi
	if err != nil {
		slog.Error("konfigurasi tidak valid", "err", err)
		os.Exit(1) // gagal start
	}
	httputil.InitLogger(cfg.AppEnv) // pasang logger

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) // batal saat Ctrl+C/SIGTERM
	defer stop()                                                                           // lepas listener sinyal

	db, err := store.Open(ctx, cfg.DBPath) // buka & migrasi order.db
	if err != nil {
		slog.Error("gagal membuka order.db", "err", err)
		os.Exit(1)
	}
	defer db.Close() // tutup database saat keluar

	repo := store.NewRepo(db)              // repository pesanan
	if err := repo.Seed(ctx); err != nil { // isi 8 pesanan contoh bila kosong
		slog.Error("seed order gagal", "err", err)
		os.Exit(1)
	}

	svc := service.New(repo, catalogclient.New(cfg.CatalogURL, cfg.InternalAPIKey, cfg.UpstreamTimeout), cfg.PaymentTTL) // logika bisnis
	go jobs.ExpirePayments(ctx, svc, 60*time.Second)                                                                     // berhenti saat ctx batal (graceful shutdown)

	mux := http.NewServeMux()                                                              // router
	handlers.Register(mux, cfg.InternalAPIKey, &handlers.Deps{Svc: svc})                   // daftarkan rute
	handler := httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)(mux) // middleware global

	if err := httputil.RunServer(ctx, "service-order", ":"+cfg.Port, handler); err != nil { // jalankan server
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
