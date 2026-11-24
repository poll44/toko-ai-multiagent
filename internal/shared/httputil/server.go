package httputil // helper HTTP bersama

import ( // package yang dipakai
	"context"  // pembatalan & timeout
	"errors"   // pengecekan jenis error
	"log/slog" // logging terstruktur
	"net/http" // server HTTP
	"os"       // akses stdout
	"strings"  // perbandingan string
	"time"     // durasi timeout
)

// InitLogger memasang slog: JSON (default) atau teks saat APP_ENV=development.
func InitLogger(appEnv string) {
	var h slog.Handler                            // handler log yang akan dipilih
	if strings.EqualFold(appEnv, "development") { // mode development (tanpa peduli huruf besar/kecil)
		h = slog.NewTextHandler(os.Stdout, nil) // log berformat teks agar mudah dibaca
	} else {
		h = slog.NewJSONHandler(os.Stdout, nil) // log berformat JSON untuk produksi
	}
	slog.SetDefault(slog.New(h)) // jadikan logger default global
}

// RunServer menjalankan http.Server dan shutdown graceful saat ctx dibatalkan (SIGINT/SIGTERM).
// Sengaja tanpa WriteTimeout agar streaming SSE tidak terputus.
func RunServer(ctx context.Context, name, addr string, h http.Handler) error {
	srv := &http.Server{ // konfigurasi server
		Addr:              addr,             // alamat listen
		Handler:           h,                // handler utama
		ReadHeaderTimeout: 5 * time.Second,  // batas waktu baca header (anti slowloris)
		IdleTimeout:       60 * time.Second, // batas koneksi keep-alive menganggur
	}
	errCh := make(chan error, 1) // channel untuk melaporkan error server
	go func() {                  // jalankan server di goroutine terpisah
		slog.Info("service listening", "service", name, "addr", addr)                         // log server mulai
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { // error selain penutupan normal
			errCh <- err // kirim error ke channel
		}
		close(errCh) // tutup channel saat server berhenti
	}()
	select { // tunggu salah satu kejadian
	case err := <-errCh: // server berhenti sendiri
		return err // kembalikan error (nil bila normal)
	case <-ctx.Done(): // ada sinyal berhenti
		slog.Info("shutting down", "service", name)                                      // log proses shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // beri waktu 10 dtk untuk request berjalan
		defer cancel()                                                                   // bebaskan resource timer
		return srv.Shutdown(shutdownCtx)                                                 // shutdown graceful
	}
}
