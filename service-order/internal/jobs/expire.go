// Package jobs: job latar belakang service-order.
package jobs

import (
	"context"
	"log/slog"
	"time"
)

// Expirer dipenuhi *service.Service.
type Expirer interface {
	ExpireDue(ctx context.Context) (int, error)
}

// ExpirePayments menjalankan ExpireDue sekali saat start lalu tiap interval; berhenti saat ctx dibatalkan.
func ExpirePayments(ctx context.Context, svc Expirer, interval time.Duration) {
	run := func() {
		n, err := svc.ExpireDue(ctx)
		switch {
		case err != nil:
			slog.Error("job expire payments gagal", "err", err)
		case n > 0:
			slog.Info("order kedaluwarsa diproses", "count", n)
		}
	}
	run()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
