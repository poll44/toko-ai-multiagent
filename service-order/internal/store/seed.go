package store

import (
	"context"
	"database/sql"
	"time"

	"toko-ai-microservices/internal/shared/sqlitex"
	"toko-ai-microservices/service-order/internal/model"
)

// Snapshot harga/nama sesuai seed katalog (PRD 02 §4).
var catalogSnapshot = map[string]struct {
	Name  string
	Price int
}{
	"PRD-001": {"Earbuds Nova X1 TWS", 349000}, "PRD-002": {"Headphone Bass Pro H200", 799000},
	"PRD-003": {"Powerbank 20000mAh FastCharge", 299000}, "PRD-004": {"Kabel USB-C to USB-C 1m 100W", 89000},
	"PRD-005": {"Charger GaN 65W 3 Port", 249000}, "PRD-006": {"Smartwatch FitBand S5", 899000},
	"PRD-007": {"Keyboard Mekanikal K87 RGB", 649000}, "PRD-008": {"Mouse Wireless Silent M330", 159000},
	"PRD-009": {"Webcam Full HD 1080p", 329000}, "PRD-011": {"Dudukan Smartphone Aluminium", 79000},
	"PRD-012": {"SSD Eksternal 1TB", 1299000},
}

type seedItem struct {
	ID  string
	Qty int
}

type seedOrder struct {
	ID, User, Status, Pay, Method, Address string
	DaysAgo                                int
	Items                                  []seedItem
	Tracking, Failure                      string
	ExpiresInMin                           int // >0: payment_expires_at = now + N menit
}

const (
	addrBudi = "Jl. Merdeka No. 10, Jakarta"
	addrSiti = "Jl. Melati No. 5, Bandung"
)

var seedOrders = []seedOrder{
	{ID: "ORD-001", User: "USR-01", Status: model.StatusDelivered, Pay: model.PayPaid, Method: "EWALLET", Address: addrBudi, DaysAgo: 20,
		Items: []seedItem{{"PRD-001", 1}, {"PRD-004", 2}}},
	{ID: "ORD-002", User: "USR-01", Status: model.StatusShipped, Pay: model.PayPaid, Method: "BANK_TRANSFER", Address: addrBudi, DaysAgo: 3,
		Items: []seedItem{{"PRD-006", 1}}, Tracking: "JNE-TRK-0002"},
	{ID: "ORD-003", User: "USR-01", Status: model.StatusPaymentFailed, Pay: model.PayFailed, Method: "EWALLET", Address: addrBudi, DaysAgo: 2,
		Items: []seedItem{{"PRD-007", 1}}, Failure: "Saldo e-wallet tidak mencukupi"},
	{ID: "ORD-004", User: "USR-01", Status: model.StatusPaymentTimeout, Pay: model.PayExpired, Method: "BANK_TRANSFER", Address: addrBudi, DaysAgo: 1,
		Items: []seedItem{{"PRD-012", 1}}, Failure: "Batas waktu pembayaran terlewati (15 menit)"},
	{ID: "ORD-005", User: "USR-02", Status: model.StatusProcessing, Pay: model.PayPaid, Method: "CREDIT_CARD", Address: addrSiti, DaysAgo: 4,
		Items: []seedItem{{"PRD-003", 2}, {"PRD-005", 1}}},
	{ID: "ORD-006", User: "USR-02", Status: model.StatusPendingPayment, Pay: model.PayUnpaid, Method: "EWALLET", Address: addrSiti, DaysAgo: 0,
		Items: []seedItem{{"PRD-002", 1}}, ExpiresInMin: 10},
	{ID: "ORD-007", User: "USR-02", Status: model.StatusCancelled, Pay: model.PayUnpaid, Method: "BANK_TRANSFER", Address: addrSiti, DaysAgo: 6,
		Items: []seedItem{{"PRD-008", 1}}},
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
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	now := time.Now()
	return sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		for _, s := range seedOrders {
			created := now.Add(-time.Duration(s.DaysAgo) * 24 * time.Hour)
			o := &model.Order{
				ID: s.ID, UserID: s.User, Status: s.Status, PaymentStatus: s.Pay, PaymentMethod: s.Method,
				ShippingAddress: s.Address, CreatedAt: sqlitex.FormatTime(created), UpdatedAt: sqlitex.FormatTime(created),
			}
			for _, it := range s.Items {
				p := catalogSnapshot[it.ID]
				o.Items = append(o.Items, model.OrderItem{ProductID: it.ID, ProductName: p.Name, UnitPrice: p.Price, Quantity: it.Qty, Subtotal: p.Price * it.Qty})
				o.TotalAmount += p.Price * it.Qty
			}
			if s.Tracking != "" {
				o.TrackingNumber = model.Ptr(s.Tracking)
			}
			if s.Failure != "" {
				o.FailureReason = model.Ptr(s.Failure)
			}
			if s.ExpiresInMin > 0 {
				o.PaymentExpiresAt = model.Ptr(sqlitex.FormatTime(now.Add(time.Duration(s.ExpiresInMin) * time.Minute)))
			}
			if s.Pay == model.PayPaid {
				o.PaidAt = model.Ptr(sqlitex.FormatTime(created.Add(time.Hour)))
			}
			if err := insertOrder(ctx, tx, o); err != nil {
				return err
			}
			path := statusPath[s.Status]
			var prev *string
			for i, st := range path {
				at := sqlitex.FormatTime(created.Add(time.Duration(i) * time.Hour))
				actor := s.User
				if i > 1 && st != model.StatusPaymentFailed && st != model.StatusPaymentTimeout {
					actor = "ADM-01"
				}
				if st == model.StatusPaymentTimeout {
					actor = "SYSTEM"
				}
				var note *string
				if i == len(path)-1 && s.Failure != "" {
					note = model.Ptr(s.Failure)
				}
				if err := addHistory(ctx, tx, s.ID, prev, st, note, actor, at); err != nil {
					return err
				}
				cp := st
				prev = &cp
			}
		}
		return nil
	})
}
