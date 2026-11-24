package store // data awal (seed) tiket

import ( // package yang dipakai
	"context"      // context
	"database/sql" // API database
	"time"         // waktu

	"toko-ai-microservices/internal/shared/sqlitex"       // transaksi & format waktu
	"toko-ai-microservices/service-ticket/internal/model" // tipe data
)

// seedTicket: bentuk data satu tiket seed.
type seedTicket struct {
	ID, User, Order, Subject, Desc, Cat, Prio, Status, Assigned, Resolution string // field teks tiket
	DaysAgo                                                                 int    // dibuat berapa hari yang lalu
	AdminReply                                                              string // balasan admin (opsional)
}

// seedTickets: 5 tiket contoh dengan berbagai status.
var seedTickets = []seedTicket{
	// TKT-001: milik USR-01
	{ID: "TKT-001", User: "USR-01", Order: "ORD-003", Cat: "PAYMENT", Prio: "HIGH", Status: model.StOpen, DaysAgo: 2,
		Subject: "Pembayaran ORD-003 gagal padahal saldo cukup",
		Desc:    "Saya sudah mengisi saldo e-wallet tetapi pembayaran pesanan ORD-003 tetap gagal dengan alasan saldo tidak mencukupi."},
	// TKT-002: milik USR-01
	{ID: "TKT-002", User: "USR-01", Order: "ORD-001", Cat: "PRODUCT", Prio: "LOW", Status: model.StResolved, Assigned: "ADM-01", DaysAgo: 15,
		Subject:    "Pertanyaan garansi earbuds",
		Desc:       "Berapa lama garansi untuk Earbuds Nova X1 yang saya beli dan bagaimana cara klaimnya?",
		Resolution: "Garansi 12 bulan untuk kerusakan pabrik; klaim dilakukan lewat tiket kategori PRODUCT dengan nomor pesanan.",
		AdminReply: "Halo, garansi earbuds 12 bulan untuk kerusakan pabrik. Silakan buat tiket PRODUCT bila perlu klaim."},
	// TKT-003: milik USR-02
	{ID: "TKT-003", User: "USR-02", Order: "ORD-005", Cat: "SHIPPING", Prio: "MEDIUM", Status: model.StInProgress, Assigned: "ADM-01", DaysAgo: 3,
		Subject:    "Estimasi pengiriman pesanan belum jelas",
		Desc:       "Pesanan ORD-005 sudah berstatus PROCESSING beberapa hari, kapan kira-kira dikirim?",
		AdminReply: "Halo Siti, pesanan Anda sedang kami proses dan akan segera dikirim; resi akan muncul saat status SHIPPED."},
	// TKT-004: milik USR-02
	{ID: "TKT-004", User: "USR-02", Cat: "TECHNICAL", Prio: "MEDIUM", Status: model.StOpen, DaysAgo: 1,
		Subject: "Halaman checkout error saat memilih e-wallet",
		Desc:    "Saat memilih metode pembayaran e-wallet di halaman checkout muncul pesan error dan halaman tidak bisa dilanjutkan."},
	// TKT-005: milik USR-02
	{ID: "TKT-005", User: "USR-02", Order: "ORD-008", Cat: "PRODUCT", Prio: "LOW", Status: model.StClosed, Assigned: "ADM-01", DaysAgo: 24,
		Subject:    "Webcam terbungkus kurang rapi",
		Desc:       "Kemasan webcam yang saya terima kurang rapi, tetapi barangnya berfungsi normal.",
		Resolution: "Terima kasih atas masukannya; kemasan akan dievaluasi.",
		AdminReply: "Terima kasih atas masukannya, kami teruskan ke tim gudang."},
}

// Seed 5 tiket contoh bila tabel kosong (idempoten), lengkap dengan pesan & history konsisten.
func (r *Repo) SeedTickets(ctx context.Context) error {
	var n int // jumlah tiket yang sudah ada
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tickets`).Scan(&n); err != nil {
		return err
	}
	if n > 0 { // sudah terisi: tidak perlu seed lagi
		return nil
	}
	now := time.Now()                                         // acuan waktu seed
	return sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error { // seed dalam satu transaksi
		for _, s := range seedTickets { // buat tiap tiket seed
			created := now.Add(-time.Duration(s.DaysAgo) * 24 * time.Hour)                                    // waktu dibuat (mundur DaysAgo hari)
			at := func(h int) string { return sqlitex.FormatTime(created.Add(time.Duration(h) * time.Hour)) } // waktu = dibuat + h jam
			t := &model.Ticket{
				ID: s.ID, UserID: s.User, Subject: s.Subject, Description: s.Desc, Category: s.Cat, Priority: s.Prio,
				Status: s.Status, CreatedAt: at(0), UpdatedAt: at(0),
			}
			if s.Order != "" { // ada pesanan terkait
				t.OrderID = model.Ptr(s.Order)
			}
			if s.Assigned != "" { // sudah ditugaskan
				t.AssignedTo = model.Ptr(s.Assigned)
			}
			if s.Resolution != "" { // ada catatan penyelesaian
				t.ResolutionNote = model.Ptr(s.Resolution)
			}
			if s.Status == model.StResolved || s.Status == model.StClosed { // sudah diselesaikan
				t.ResolvedAt = model.Ptr(at(5))
			}
			if s.Status == model.StClosed { // sudah ditutup
				t.ClosedAt = model.Ptr(at(8))
			}
			if err := insertTicket(ctx, tx, t); err != nil { // simpan tiket
				return err
			}
			if err := insertMessage(ctx, tx, s.ID, s.User, "CUSTOMER", s.Desc, at(0)); err != nil { // pesan awal dari pelanggan
				return err
			}
			if s.AdminReply != "" { // balasan admin (bila ada)
				if err := insertMessage(ctx, tx, s.ID, s.Assigned, "ADMIN_SISTEM", s.AdminReply, at(2)); err != nil {
					return err
				}
			}
			// history konsisten dengan status akhir
			path := []string{model.StOpen} // jalur status menuju status akhir
			switch s.Status {
			case model.StInProgress:
				path = append(path, model.StInProgress)
			case model.StResolved:
				path = append(path, model.StInProgress, model.StResolved)
			case model.StClosed:
				path = append(path, model.StInProgress, model.StResolved, model.StClosed)
			}
			var prev *string          // status sebelumnya
			for i, st := range path { // buat riwayat tiap langkah
				actor := s.User // default pelaku: pemilik tiket
				if i > 0 {      // langkah lanjutan dikerjakan admin
					actor = s.Assigned
				}
				if st == model.StClosed { // penutupan dilakukan pemilik
					actor = s.User
				}
				if err := insertHistory(ctx, tx, s.ID, prev, st, actor, at(i*2)); err != nil {
					return err
				}
				cp := st // salinan agar pointer aman
				prev = &cp
			}
		}
		return nil
	})
}
