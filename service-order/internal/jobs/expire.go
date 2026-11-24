// Package jobs: job latar belakang service-order.
package jobs // nama package: jobs

import ( // package yang dipakai
	"context"  // context
	"log/slog" // logging terstruktur
	"time"     // ticker
)

// Expirer dipenuhi *service.Service.
type Expirer interface {
	ExpireDue(ctx context.Context) (int, error) // proses pesanan yang lewat batas bayar
}

// ExpirePayments menjalankan ExpireDue sekali saat start lalu tiap interval; berhenti saat ctx dibatalkan.
func ExpirePayments(ctx context.Context, svc Expirer, interval time.Duration) {
	run := func() { // satu putaran pemrosesan
		n, err := svc.ExpireDue(ctx) // tandai pesanan kedaluwarsa & kembalikan stok
		switch {
		case err != nil: // gagal
			slog.Error("job expire payments gagal", "err", err)
		case n > 0: // ada yang diproses
			slog.Info("order kedaluwarsa diproses", "count", n)
		}
	}
	run()                         // jalankan sekali saat start
	t := time.NewTicker(interval) // jadwal putaran berikutnya
	defer t.Stop()                // hentikan ticker saat keluar
	for {
		select {
		case <-ctx.Done(): // service berhenti
			return
		case <-t.C: // waktunya putaran baru
			run()
		}
	}
}
