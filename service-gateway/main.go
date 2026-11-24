// service-gateway (:8080) — autentikasi JWT, injeksi header identitas, reverse proxy. Lihat docs/trd/01-SERVICE-GATEWAY.md
package main // titik masuk program service-gateway

import ( // package yang dipakai
	"context"   // context & pembatalan
	"log/slog"  // logging terstruktur
	"os"        // exit
	"os/signal" // menangkap sinyal OS
	"syscall"   // konstanta SIGTERM

	"toko-ai-microservices/internal/shared/httputil"          // helper HTTP bersama
	"toko-ai-microservices/service-gateway/internal/config"   // konfigurasi
	"toko-ai-microservices/service-gateway/internal/handlers" // routing gateway
	"toko-ai-microservices/service-gateway/internal/store"    // akses auth.db
)

// main: memuat config, membuka auth.db, merakit handler gateway, lalu menjalankan server.
func main() {
	cfg, err := config.Load() // baca konfigurasi
	if err != nil {
		slog.Error("konfigurasi tidak valid", "err", err)
		os.Exit(1) // gagal start
	}
	httputil.InitLogger(cfg.AppEnv) // pasang logger

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) // batal saat Ctrl+C/SIGTERM
	defer stop()                                                                           // lepas listener sinyal

	db, err := store.Open(ctx, cfg.AuthDBPath) // buka & migrasi auth.db
	if err != nil {
		slog.Error("gagal membuka auth.db", "err", err)
		os.Exit(1)
	}
	defer db.Close() // tutup database saat keluar

	h := handlers.Handler(&handlers.Deps{Cfg: cfg, Users: store.NewRepo(db)})           // rakit seluruh handler
	if err := httputil.RunServer(ctx, "service-gateway", ":"+cfg.Port, h); err != nil { // jalankan server
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
