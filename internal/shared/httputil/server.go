package httputil

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// InitLogger memasang slog: JSON (default) atau teks saat APP_ENV=development.
func InitLogger(appEnv string) {
	var h slog.Handler
	if strings.EqualFold(appEnv, "development") {
		h = slog.NewTextHandler(os.Stdout, nil)
	} else {
		h = slog.NewJSONHandler(os.Stdout, nil)
	}
	slog.SetDefault(slog.New(h))
}

// RunServer menjalankan http.Server dan shutdown graceful saat ctx dibatalkan (SIGINT/SIGTERM).
// Sengaja tanpa WriteTimeout agar streaming SSE tidak terputus.
func RunServer(ctx context.Context, name, addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("service listening", "service", name, "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down", "service", name)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
