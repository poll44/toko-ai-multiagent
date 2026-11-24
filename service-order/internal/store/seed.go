package store // data awal (seed) pesanan

import ( // package yang dipakai
	"context"      // context
	"database/sql" // API database
	"time"         // waktu

	"toko-ai-microservices/internal/shared/sqlitex"      // transaksi & format waktu
	"toko-ai-microservices/service-order/internal/model" // tipe data
)

// Snapshot harga/nama sesuai seed katalog (TRD 02 §4).
var catalogSnapshot = map[string]struct {
	Name  string // nama produk
	Price int    // harga produk
}{
	"PRD-001": {"Earbuds Nova X1 TWS", 349000}, "PRD-002": {"Headphone Bass Pro H200", 799000},
	"PRD-003": {"Powerbank 20000mAh FastCharge", 299000}, "PRD-004": {"Kabel USB-C to USB-C 1m 100W", 89000},
	"PRD-005": {"Charger GaN 65W 3 Port", 249000}, "PRD-006": {"Smartwatch FitBand S5", 899000},
	"PRD-007": {"Keyboard Mekanikal K87 RGB", 649000}, "PRD-008": {"Mouse Wireless Silent M330", 159000},
	"PRD-009": {"Webcam Full HD 1080p", 329000}, "PRD-011": {"Dudukan Smartphone Aluminium", 79000},
	"PRD-012": {"SSD Eksternal 1TB", 1299000},
}

// seedItem: satu barang pada pesanan seed.
type seedItem struct {
	ID  string // ID produk
	Qty int    // jumlah
}

// seedOrder: bentuk data satu pesanan seed.
type seedOrder struct {
	ID, User, Status, Pay, Method, Address string     // ID, pemilik, status, status bayar, metode, alamat
	DaysAgo                                int        // dibuat berapa hari yang lalu
	Items                                  []seedItem // daftar barang
	Tracking, Failure                      string     // nomor resi & alasan gagal (opsional)
	ExpiresInMin                           int        // >0: payment_expires_at = now + N menit
}

// Alamat pengiriman contoh.
const (
	addrBudi = "Jl. Merdeka No. 10, Jakarta" // alamat Budi
	addrSiti = "Jl. Melati No. 5, Bandung"   // alamat Siti
)

// seedOrders: 8 pesanan contoh yang mencakup berbagai status.
var seedOrders = []seedOrder{
	// ORD-001: milik Budi, sudah diterima
	{ID: "ORD-001", User: "USR-01", Status: model.StatusDelivered, Pay: model.PayPaid, Method: "EWALLET", Address: addrBudi, DaysAgo: 20,
		Items: []seedItem{{"PRD-001", 1}, {"PRD-004", 2}}},
	// ORD-002: milik Budi, sedang dikirim (ada resi)
	{ID: "ORD-002", User: "USR-01", Status: model.StatusShipped, Pay: model.PayPaid, Method: "BANK_TRANSFER", Address: addrBudi, DaysAgo: 3,
		Items: []seedItem{{"PRD-006", 1}}, Tracking: "JNE-TRK-0002"},
	// ORD-003: milik Budi, pembayaran gagal (saldo kurang)
	{ID: "ORD-003", User: "USR-01", Status: model.StatusPaymentFailed, Pay: model.PayFailed, Method: "EWALLET", Address: addrBudi, DaysAgo: 2,
		Items: []seedItem{{"PRD-007", 1}}, Failure: "Saldo e-wallet tidak mencukupi"},
	// ORD-004: milik Budi, pembayaran kedaluwarsa
	{ID: "ORD-004", User: "USR-01", Status: model.StatusPaymentTimeout, Pay: model.PayExpired, Method: "BANK_TRANSFER", Address: addrBudi, DaysAgo: 1,
		Items: []seedItem{{"PRD-012", 1}}, Failure: "Batas waktu pembayaran terlewati (15 menit)"},
	// ORD-005: milik Siti, sedang diproses
	{ID: "ORD-005", User: "USR-02", Status: model.StatusProcessing, Pay: model.PayPaid, Method: "CREDIT_CARD", Address: addrSiti, DaysAgo: 4,
		Items: []seedItem{{"PRD-003", 2}, {"PRD-005", 1}}},
	// ORD-006: milik Siti, menunggu pembayaran (kedaluwarsa 10 menit lagi)
	{ID: "ORD-006", User: "USR-02", Status: model.StatusPendingPayment, Pay: model.PayUnpaid, Method: "EWALLET", Address: addrSiti, DaysAgo: 0,
		Items: []seedItem{{"PRD-002", 1}}, ExpiresInMin: 10},
	// ORD-007: milik Siti, dibatalkan
	{ID: "ORD-007", User: "USR-02", Status: model.StatusCancelled, Pay: model.PayUnpaid, Method: "BANK_TRANSFER", Address: addrSiti, DaysAgo: 6,
		Items: []seedItem{{"PRD-008", 1}}},
	// ORD-008: milik Siti, sudah diterima
	{ID: "ORD-008", User: "USR-02", Status: model.StatusDelivered, Pay: model.PayPaid, Method: "EWALLET", Address: addrSiti, DaysAgo: 25,
		Items: []seedItem{{"PRD-009", 1}, {"PRD-011", 1}}},
}

// Jalur status untuk history seed yang konsisten.
var statusPath = map[string][]string{
	model.StatusPendingPayment: {model.StatusPendingPayment},
	model.StatusProcessing:     {model.StatusPendingPayment, model.StatusPaid, model.StatusProcessing},
	model.StatusShipped:        {model.StatusPendingPayment, model.StatusPaid, model.StatusProcessing, model.StatusShipped},
	model.StatusDelivered:      {model.StatusPendingPayment, model.StatusPaid, model.StatusProcessing, model.StatusShipped, model.StatusDelivered},
	model.StatusPaymentFailed:  {model.StatusPendingPayment, model.StatusPaymentFailed},
	model.StatusPaymentTimeout: {model.StatusPendingPayment, model.StatusPaymentTimeout},
	model.StatusCancelled:      {model.StatusPendingPayment, model.StatusCancelled},
}

// Seed mengisi 8 order contoh bila tabel kosong (idempoten). Tidak memanggil katalog.
func (r *Repo) Seed(ctx context.Context) error {
	var n int // jumlah pesanan yang sudah ada
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders`).Scan(&n); err != nil {
		return err
	}
	if n > 0 { // sudah terisi: tidak perlu seed lagi
		return nil
	}
	now := time.Now()                                         // acuan waktu seed
	return sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error { // seed dalam satu transaksi
		for _, s := range seedOrders { // buat tiap pesanan seed
			created := now.Add(-time.Duration(s.DaysAgo) * 24 * time.Hour) // waktu dibuat (mundur DaysAgo hari)
			o := &model.Order{
				ID: s.ID, UserID: s.User, Status: s.Status, PaymentStatus: s.Pay, PaymentMethod: s.Method,
				ShippingAddress: s.Address, CreatedAt: sqlitex.FormatTime(created), UpdatedAt: sqlitex.FormatTime(created),
			}
			for _, it := range s.Items { // isi item dari snapshot katalog
				p := catalogSnapshot[it.ID]
				o.Items = append(o.Items, model.OrderItem{ProductID: it.ID, ProductName: p.Name, UnitPrice: p.Price, Quantity: it.Qty, Subtotal: p.Price * it.Qty})
				o.TotalAmount += p.Price * it.Qty // akumulasi total
			}
			if s.Tracking != "" { // ada nomor resi
				o.TrackingNumber = model.Ptr(s.Tracking)
			}
			if s.Failure != "" { // ada alasan gagal
				o.FailureReason = model.Ptr(s.Failure)
			}
			if s.ExpiresInMin > 0 { // batas bayar relatif terhadap sekarang
				o.PaymentExpiresAt = model.Ptr(sqlitex.FormatTime(now.Add(time.Duration(s.ExpiresInMin) * time.Minute)))
			}
			if s.Pay == model.PayPaid { // sudah dibayar: dianggap 1 jam setelah dibuat
				o.PaidAt = model.Ptr(sqlitex.FormatTime(created.Add(time.Hour)))
			}
			if err := insertOrder(ctx, tx, o); err != nil { // simpan pesanan & item
				return err
			}
			path := statusPath[s.Status] // jalur status menuju status akhir
			var prev *string             // status sebelumnya
			for i, st := range path {    // buat riwayat tiap langkah
				at := sqlitex.FormatTime(created.Add(time.Duration(i) * time.Hour))               // tiap langkah selang 1 jam
				actor := s.User                                                                   // default pelaku: pemilik pesanan
				if i > 1 && st != model.StatusPaymentFailed && st != model.StatusPaymentTimeout { // tahap lanjutan dikerjakan admin
					actor = "ADM-01"
				}
				if st == model.StatusPaymentTimeout { // kedaluwarsa dicatat oleh sistem
					actor = "SYSTEM"
				}
				var note *string
				if i == len(path)-1 && s.Failure != "" { // catatan alasan gagal pada langkah terakhir
					note = model.Ptr(s.Failure)
				}
				if err := addHistory(ctx, tx, s.ID, prev, st, note, actor, at); err != nil {
					return err
				}
				cp := st // salinan agar pointer aman
				prev = &cp
			}
		}
		return nil
	})
}
