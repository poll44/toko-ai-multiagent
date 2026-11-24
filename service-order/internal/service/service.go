// Package service: logika pesanan — state machine, orkestrasi stok dengan service-catalog, kompensasi (TRD 03 §4, §6).
package service // nama package: service

import ( // package yang dipakai
	"context"  // context
	"errors"   // errors.As
	"log/slog" // logging terstruktur
	"sync"     // mutex
	"time"     // waktu

	"toko-ai-microservices/internal/shared/httputil"             // identitas & error API
	"toko-ai-microservices/internal/shared/sqlitex"              // format waktu
	"toko-ai-microservices/internal/shared/svcclient"            // UpstreamError
	"toko-ai-microservices/service-order/internal/catalogclient" // klien catalog
	"toko-ai-microservices/service-order/internal/model"         // tipe data
	"toko-ai-microservices/service-order/internal/store"         // repository
)

// Catalog diimplementasi *catalogclient.Client (dan fake pada test).
type Catalog interface {
	Decrement(ctx context.Context, referenceID string, items []catalogclient.Item) ([]catalogclient.ReservedItem, error) // kurangi stok
	Restock(ctx context.Context, referenceID, reason string, items []catalogclient.Item) error                           // kembalikan stok
}

// Service berisi logika bisnis pesanan.
type Service struct {
	repo     *store.Repo      // akses database
	catalog  Catalog          // klien service-catalog
	ttl      time.Duration    // batas waktu pembayaran
	now      func() time.Time // sumber waktu (bisa diganti saat test)
	createMu sync.Mutex       // menserialkan: generate id → decrement stok → insert (id dipakai sebagai reference_id)
}

// New membuat Service baru.
func New(repo *store.Repo, catalog Catalog, paymentTTL time.Duration) *Service {
	return &Service{repo: repo, catalog: catalog, ttl: paymentTTL, now: time.Now}
}

// SetClock mengganti sumber waktu (untuk test).
func (s *Service) SetClock(f func() time.Time) { s.now = f }

// allowed: transisi sah (TRD 03 §4). Pemeriksaan pelaku dilakukan di tiap operasi.
var allowed = map[string][]string{
	model.StatusPendingPayment: {model.StatusPaid, model.StatusPaymentFailed, model.StatusPaymentTimeout, model.StatusCancelled}, // dari menunggu bayar
	model.StatusPaid:           {model.StatusProcessing, model.StatusCancelled},                                                  // dari sudah dibayar
	model.StatusProcessing:     {model.StatusShipped, model.StatusCancelled},                                                     // dari diproses
	model.StatusShipped:        {model.StatusDelivered},                                                                          // dari dikirim
}

// canMove: true bila transisi status from -> to diizinkan.
func canMove(from, to string) bool {
	for _, t := range allowed[from] { // cek tiap tujuan sah
		if t == to {
			return true
		}
	}
	return false
}

// invalidTransition membuat error 409 untuk transisi status yang tidak sah.
func invalidTransition(from, to string) error {
	return httputil.ErrConflict("INVALID_STATE_TRANSITION", "Status pesanan tidak dapat diubah dari "+from+" ke "+to+".",
		map[string]any{"from": from, "to": to})
}

// isOwner: true bila pemanggil adalah pemilik pesanan.
func isOwner(id httputil.Identity, o *model.Order) bool { return o.UserID == id.UserID }

// ---------- Create ----------

// ItemInput: satu barang yang dipesan.
type ItemInput struct {
	ProductID string `json:"product_id"` // ID produk
	Quantity  int    `json:"quantity"`   // jumlah
}

// CreateInput: data untuk membuat pesanan.
type CreateInput struct {
	Items           []ItemInput // barang yang dipesan
	PaymentMethod   string      // metode pembayaran
	ShippingAddress string      // alamat pengiriman
}

// Create membuat pesanan: reservasi stok di catalog, simpan pesanan, dan kompensasi (restock) bila penyimpanan gagal.
func (s *Service) Create(ctx context.Context, id httputil.Identity, in CreateInput) (*model.Order, error) {
	s.createMu.Lock()         // serialkan pembuatan pesanan
	defer s.createMu.Unlock() // lepas kunci saat selesai

	orderID, err := s.repo.NextID(ctx) // ID pesanan berikutnya
	if err != nil {
		return nil, err
	}
	req := make([]catalogclient.Item, len(in.Items)) // permintaan ke catalog
	for i, it := range in.Items {
		req[i] = catalogclient.Item{ProductID: it.ProductID, Quantity: it.Quantity}
	}
	// Harga/nama SELALU dari katalog (snapshot); harga dari klien tidak pernah dipakai.
	reserved, err := s.catalog.Decrement(ctx, orderID, req) // kurangi stok
	if err != nil {
		var ue *svcclient.UpstreamError
		if errors.As(err, &ue) { // error dari catalog
			return nil, ue.APIError() // 409 INSUFFICIENT_STOCK / 404 / 422 / 502 / 504 diteruskan
		}
		return nil, err
	}

	now := s.now() // waktu pembuatan
	o := &model.Order{
		ID: orderID, UserID: id.UserID, Status: model.StatusPendingPayment, PaymentStatus: model.PayUnpaid,
		PaymentMethod: in.PaymentMethod, ShippingAddress: in.ShippingAddress,
		PaymentExpiresAt: model.Ptr(sqlitex.FormatTime(now.Add(s.ttl))), // batas bayar = sekarang + TTL
		CreatedAt:        sqlitex.FormatTime(now), UpdatedAt: sqlitex.FormatTime(now),
	}
	for _, r := range reserved { // bangun item dari hasil reservasi
		sub := r.UnitPrice * r.Quantity // subtotal
		o.Items = append(o.Items, model.OrderItem{ProductID: r.ProductID, ProductName: r.Name, UnitPrice: r.UnitPrice, Quantity: r.Quantity, Subtotal: sub})
		o.TotalAmount += sub // akumulasi total
	}
	if err := s.repo.Create(ctx, o, id.UserID); err != nil { // simpan pesanan
		// KOMPENSASI: kembalikan stok yang sudah dipotong.
		s.restock(ctx, orderID, "ORDER_CREATE_FAILED", req)
		return nil, err
	}
	return s.repo.Get(ctx, orderID, true) // kembalikan pesanan lengkap
}

// ---------- Read ----------

// Get: CUSTOMER non-pemilik => 404 (eksistensi tidak bocor).
func (s *Service) Get(ctx context.Context, id httputil.Identity, orderID string) (*model.Order, error) {
	o, err := s.repo.Get(ctx, orderID, true) // ambil pesanan + riwayat
	if err != nil {
		return nil, err
	}
	if id.Role == httputil.RoleCustomer && !isOwner(id, o) { // bukan pemilik
		return nil, httputil.ErrNotFound("Pesanan " + orderID + " tidak ditemukan.")
	}
	return o, nil
}

// List meneruskan pencarian daftar pesanan ke repository.
func (s *Service) List(ctx context.Context, f model.ListFilter) ([]model.Order, int, error) {
	return s.repo.List(ctx, f)
}

// ---------- Pay ----------

// Alasan standar untuk pembayaran gagal/kedaluwarsa.
const (
	reasonDefaultFail = "Pembayaran ditolak oleh penyedia pembayaran" // alasan gagal bawaan
	reasonTimeout     = "Batas waktu pembayaran terlewati (15 menit)" // alasan kedaluwarsa
)

// Pay mensimulasikan pembayaran (success|fail|timeout). Order kedaluwarsa otomatis menjadi PAYMENT_TIMEOUT (+restock) lalu 409.
func (s *Service) Pay(ctx context.Context, id httputil.Identity, orderID, simulate, failReason string) (*model.Order, error) {
	var expired, restockNeeded bool // penanda hasil di dalam transaksi
	o, err := s.repo.Apply(ctx, orderID, id.UserID, func(o *model.Order) (string, error) {
		if !isOwner(id, o) { // hanya pemilik yang boleh membayar
			return "", httputil.ErrNotFound("Pesanan " + orderID + " tidak ditemukan.")
		}
		if o.Status != model.StatusPendingPayment { // hanya yang menunggu bayar
			return "", invalidTransition(o.Status, model.StatusPaid)
		}
		if o.PaymentExpiresAt != nil && *o.PaymentExpiresAt < sqlitex.FormatTime(s.now()) { // sudah lewat batas bayar
			expired, restockNeeded = true, true
			markTimeout(o)
			return reasonTimeout, nil
		}
		switch simulate {
		case "success": // bayar berhasil
			o.Status, o.PaymentStatus = model.StatusPaid, model.PayPaid
			o.PaidAt, o.PaymentExpiresAt = model.Ptr(sqlitex.FormatTime(s.now())), nil
			return "", nil
		case "fail": // bayar gagal
			reason := failReason
			if reason == "" { // pakai alasan bawaan
				reason = reasonDefaultFail
			}
			o.Status, o.PaymentStatus, o.FailureReason, o.PaymentExpiresAt = model.StatusPaymentFailed, model.PayFailed, &reason, nil
			restockNeeded = true
			return reason, nil
		default: // timeout
			markTimeout(o)
			restockNeeded = true
			return reasonTimeout, nil
		}
	})
	if err != nil {
		return nil, err
	}
	if restockNeeded { // kembalikan stok untuk pembayaran gagal/timeout
		s.restockOrder(ctx, o, "PAYMENT_"+map[bool]string{true: "TIMEOUT", false: "FAILED"}[o.Status == model.StatusPaymentTimeout])
	}
	if expired { // pesanan sudah kedaluwarsa saat dibayar
		return nil, invalidTransition(model.StatusPendingPayment, model.StatusPaid)
	}
	return o, nil
}

// markTimeout mengubah pesanan menjadi PAYMENT_TIMEOUT / EXPIRED.
func markTimeout(o *model.Order) {
	o.Status, o.PaymentStatus, o.FailureReason, o.PaymentExpiresAt = model.StatusPaymentTimeout, model.PayExpired, model.Ptr(reasonTimeout), nil
}

// ---------- Cancel ----------

// Cancel: CUSTOMER hanya PENDING_PAYMENT miliknya; ADMIN_SISTEM PENDING_PAYMENT/PAID/PROCESSING (PAID => REFUNDED).
func (s *Service) Cancel(ctx context.Context, id httputil.Identity, orderID, reason string) (*model.Order, error) {
	o, err := s.repo.Apply(ctx, orderID, id.UserID, func(o *model.Order) (string, error) {
		if id.Role == httputil.RoleCustomer { // aturan untuk pelanggan
			if !isOwner(id, o) { // bukan pemilik
				return "", httputil.ErrNotFound("Pesanan " + orderID + " tidak ditemukan.")
			}
			if o.Status != model.StatusPendingPayment { // hanya yang belum dibayar
				return "", invalidTransition(o.Status, model.StatusCancelled)
			}
		} else if !canMove(o.Status, model.StatusCancelled) { // admin: ikuti tabel transisi
			return "", invalidTransition(o.Status, model.StatusCancelled)
		}
		if o.PaymentStatus == model.PayPaid { // sudah dibayar => refund
			o.PaymentStatus = model.PayRefunded
		}
		o.Status, o.PaymentExpiresAt = model.StatusCancelled, nil // tandai dibatalkan
		return reason, nil
	})
	if err != nil {
		return nil, err
	}
	s.restockOrder(ctx, o, "ORDER_CANCELLED") // kembalikan stok
	return o, nil
}

// ---------- Fulfilment (admin) ----------

// SetStatus: admin mengubah status pemenuhan (PROCESSING/SHIPPED/DELIVERED); SHIPPED menyimpan nomor resi.
func (s *Service) SetStatus(ctx context.Context, id httputil.Identity, orderID, status, tracking, note string) (*model.Order, error) {
	return s.repo.Apply(ctx, orderID, id.UserID, func(o *model.Order) (string, error) {
		switch status {
		case model.StatusProcessing, model.StatusShipped, model.StatusDelivered: // status fulfilment yang valid
		default:
			return "", httputil.ErrValidation("Status hanya boleh PROCESSING, SHIPPED, atau DELIVERED.",
				[]httputil.FieldError{{Field: "status", Message: "tidak valid untuk fulfilment"}})
		}
		if !canMove(o.Status, status) { // transisi harus sah
			return "", invalidTransition(o.Status, status)
		}
		if status == model.StatusShipped { // simpan nomor resi
			o.TrackingNumber = &tracking
		}
		o.Status = status
		return note, nil
	})
}

// ---------- Expiry ----------

// ExpireDue menandai order PENDING_PAYMENT yang lewat batas bayar sebagai PAYMENT_TIMEOUT dan mengembalikan stok.
// Identitas SYSTEM; idempoten (restock idempoten di catalog). Mengembalikan jumlah order yang diproses.
func (s *Service) ExpireDue(ctx context.Context) (int, error) {
	ids, err := s.repo.ExpiredPending(ctx, sqlitex.FormatTime(s.now())) // pesanan yang sudah lewat batas
	if err != nil {
		return 0, err
	}
	sys := httputil.Identity{UserID: "SYSTEM", Role: httputil.RoleAdminSistem} // identitas sistem
	ctx = httputil.WithIdentity(ctx, sys)
	n := 0 // jumlah yang diproses
	for _, oid := range ids {
		o, err := s.repo.Apply(ctx, oid, "SYSTEM", func(o *model.Order) (string, error) {
			if o.Status != model.StatusPendingPayment { // sudah berubah oleh request lain
				return "", invalidTransition(o.Status, model.StatusPaymentTimeout)
			}
			markTimeout(o)
			return reasonTimeout, nil
		})
		if err != nil {
			slog.Warn("expire order dilewati", "order_id", oid, "err", err)
			continue
		}
		s.restockOrder(ctx, o, "PAYMENT_TIMEOUT") // kembalikan stok
		n++
	}
	return n, nil
}

// ---------- restock ----------

// restockOrder mengembalikan stok semua item pada pesanan o.
func (s *Service) restockOrder(ctx context.Context, o *model.Order, reason string) {
	items := make([]catalogclient.Item, len(o.Items)) // ubah item pesanan ke format catalog
	for i, it := range o.Items {
		items[i] = catalogclient.Item{ProductID: it.ProductID, Quantity: it.Quantity}
	}
	s.restock(ctx, o.ID, reason, items)
}

// restock memanggil catalog (idempoten, retry 2x di client). Kegagalan hanya di-log: status order sudah final.
func (s *Service) restock(ctx context.Context, orderID, reason string, items []catalogclient.Item) {
	if err := s.catalog.Restock(context.WithoutCancel(ctx), orderID, reason, items); err != nil { // tidak ikut dibatalkan bila request klien putus
		slog.Error("restock gagal — stok perlu direkonsiliasi", "order_id", orderID, "reason", reason, "err", err)
	}
}
