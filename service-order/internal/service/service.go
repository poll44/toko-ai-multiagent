// Package service: logika pesanan — state machine, orkestrasi stok dengan service-catalog, kompensasi (PRD 03 §4, §6).
package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/sqlitex"
	"toko-ai-microservices/internal/shared/svcclient"
	"toko-ai-microservices/service-order/internal/catalogclient"
	"toko-ai-microservices/service-order/internal/model"
	"toko-ai-microservices/service-order/internal/store"
)

// Catalog diimplementasi *catalogclient.Client (dan fake pada test).
type Catalog interface {
	Decrement(ctx context.Context, referenceID string, items []catalogclient.Item) ([]catalogclient.ReservedItem, error)
	Restock(ctx context.Context, referenceID, reason string, items []catalogclient.Item) error
}

type Service struct {
	repo     *store.Repo
	catalog  Catalog
	ttl      time.Duration
	now      func() time.Time
	createMu sync.Mutex // menserialkan: generate id → decrement stok → insert (id dipakai sebagai reference_id)
}

func New(repo *store.Repo, catalog Catalog, paymentTTL time.Duration) *Service {
	return &Service{repo: repo, catalog: catalog, ttl: paymentTTL, now: time.Now}
}

// SetClock mengganti sumber waktu (untuk test).
func (s *Service) SetClock(f func() time.Time) { s.now = f }

// allowed: transisi sah (PRD 03 §4). Pemeriksaan pelaku dilakukan di tiap operasi.
var allowed = map[string][]string{
	model.StatusPendingPayment: {model.StatusPaid, model.StatusPaymentFailed, model.StatusPaymentTimeout, model.StatusCancelled},
	model.StatusPaid:           {model.StatusProcessing, model.StatusCancelled},
	model.StatusProcessing:     {model.StatusShipped, model.StatusCancelled},
	model.StatusShipped:        {model.StatusDelivered},
}

func canMove(from, to string) bool {
	for _, t := range allowed[from] {
		if t == to {
			return true
		}
	}
	return false
}

func invalidTransition(from, to string) error {
	return httputil.ErrConflict("INVALID_STATE_TRANSITION", "Status pesanan tidak dapat diubah dari "+from+" ke "+to+".",
		map[string]any{"from": from, "to": to})
}

func isOwner(id httputil.Identity, o *model.Order) bool { return o.UserID == id.UserID }

// ---------- Create ----------

type ItemInput struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type CreateInput struct {
	Items           []ItemInput
	PaymentMethod   string
	ShippingAddress string
}

func (s *Service) Create(ctx context.Context, id httputil.Identity, in CreateInput) (*model.Order, error) {
	s.createMu.Lock()
	defer s.createMu.Unlock()

	orderID, err := s.repo.NextID(ctx)
	if err != nil {
		return nil, err
	}
	req := make([]catalogclient.Item, len(in.Items))
	for i, it := range in.Items {
		req[i] = catalogclient.Item{ProductID: it.ProductID, Quantity: it.Quantity}
	}
	// Harga/nama SELALU dari katalog (snapshot); harga dari klien tidak pernah dipakai.
	reserved, err := s.catalog.Decrement(ctx, orderID, req)
	if err != nil {
		var ue *svcclient.UpstreamError
		if errors.As(err, &ue) {
			return nil, ue.APIError() // 409 INSUFFICIENT_STOCK / 404 / 422 / 502 / 504 diteruskan
		}
		return nil, err
	}

	now := s.now()
	o := &model.Order{
		ID: orderID, UserID: id.UserID, Status: model.StatusPendingPayment, PaymentStatus: model.PayUnpaid,
		PaymentMethod: in.PaymentMethod, ShippingAddress: in.ShippingAddress,
		PaymentExpiresAt: model.Ptr(sqlitex.FormatTime(now.Add(s.ttl))),
		CreatedAt:        sqlitex.FormatTime(now), UpdatedAt: sqlitex.FormatTime(now),
	}
	for _, r := range reserved {
		sub := r.UnitPrice * r.Quantity
		o.Items = append(o.Items, model.OrderItem{ProductID: r.ProductID, ProductName: r.Name, UnitPrice: r.UnitPrice, Quantity: r.Quantity, Subtotal: sub})
		o.TotalAmount += sub
	}
	if err := s.repo.Create(ctx, o, id.UserID); err != nil {
		// KOMPENSASI: kembalikan stok yang sudah dipotong.
		s.restock(ctx, orderID, "ORDER_CREATE_FAILED", req)
		return nil, err
	}
	return s.repo.Get(ctx, orderID, true)
}

// ---------- Read ----------

// Get: CUSTOMER non-pemilik => 404 (eksistensi tidak bocor).
func (s *Service) Get(ctx context.Context, id httputil.Identity, orderID string) (*model.Order, error) {
	o, err := s.repo.Get(ctx, orderID, true)
	if err != nil {
		return nil, err
	}
	if id.Role == httputil.RoleCustomer && !isOwner(id, o) {
		return nil, httputil.ErrNotFound("Pesanan " + orderID + " tidak ditemukan.")
	}
	return o, nil
}

func (s *Service) List(ctx context.Context, f model.ListFilter) ([]model.Order, int, error) {
	return s.repo.List(ctx, f)
}

// ---------- Pay ----------

const (
	reasonDefaultFail = "Pembayaran ditolak oleh penyedia pembayaran"
	reasonTimeout     = "Batas waktu pembayaran terlewati (15 menit)"
)

// Pay mensimulasikan pembayaran (success|fail|timeout). Order kedaluwarsa otomatis menjadi PAYMENT_TIMEOUT (+restock) lalu 409.
func (s *Service) Pay(ctx context.Context, id httputil.Identity, orderID, simulate, failReason string) (*model.Order, error) {
	var expired, restockNeeded bool
	o, err := s.repo.Apply(ctx, orderID, id.UserID, func(o *model.Order) (string, error) {
		if !isOwner(id, o) {
			return "", httputil.ErrNotFound("Pesanan " + orderID + " tidak ditemukan.")
		}
		if o.Status != model.StatusPendingPayment {
			return "", invalidTransition(o.Status, model.StatusPaid)
		}
		if o.PaymentExpiresAt != nil && *o.PaymentExpiresAt < sqlitex.FormatTime(s.now()) {
			expired, restockNeeded = true, true
			markTimeout(o)
			return reasonTimeout, nil
		}
		switch simulate {
		case "success":
			o.Status, o.PaymentStatus = model.StatusPaid, model.PayPaid
			o.PaidAt, o.PaymentExpiresAt = model.Ptr(sqlitex.FormatTime(s.now())), nil
			return "", nil
		case "fail":
			reason := failReason
			if reason == "" {
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
	if restockNeeded {
		s.restockOrder(ctx, o, "PAYMENT_"+map[bool]string{true: "TIMEOUT", false: "FAILED"}[o.Status == model.StatusPaymentTimeout])
	}
	if expired {
		return nil, invalidTransition(model.StatusPendingPayment, model.StatusPaid)
	}
	return o, nil
}

func markTimeout(o *model.Order) {
	o.Status, o.PaymentStatus, o.FailureReason, o.PaymentExpiresAt = model.StatusPaymentTimeout, model.PayExpired, model.Ptr(reasonTimeout), nil
}

// ---------- Cancel ----------

// Cancel: CUSTOMER hanya PENDING_PAYMENT miliknya; ADMIN_SISTEM PENDING_PAYMENT/PAID/PROCESSING (PAID => REFUNDED).
func (s *Service) Cancel(ctx context.Context, id httputil.Identity, orderID, reason string) (*model.Order, error) {
	o, err := s.repo.Apply(ctx, orderID, id.UserID, func(o *model.Order) (string, error) {
		if id.Role == httputil.RoleCustomer {
			if !isOwner(id, o) {
				return "", httputil.ErrNotFound("Pesanan " + orderID + " tidak ditemukan.")
			}
			if o.Status != model.StatusPendingPayment {
				return "", invalidTransition(o.Status, model.StatusCancelled)
			}
		} else if !canMove(o.Status, model.StatusCancelled) {
			return "", invalidTransition(o.Status, model.StatusCancelled)
		}
		if o.PaymentStatus == model.PayPaid {
			o.PaymentStatus = model.PayRefunded
		}
		o.Status, o.PaymentExpiresAt = model.StatusCancelled, nil
		return reason, nil
	})
	if err != nil {
		return nil, err
	}
	s.restockOrder(ctx, o, "ORDER_CANCELLED")
	return o, nil
}

// ---------- Fulfilment (admin) ----------

func (s *Service) SetStatus(ctx context.Context, id httputil.Identity, orderID, status, tracking, note string) (*model.Order, error) {
	return s.repo.Apply(ctx, orderID, id.UserID, func(o *model.Order) (string, error) {
		switch status {
		case model.StatusProcessing, model.StatusShipped, model.StatusDelivered:
		default:
			return "", httputil.ErrValidation("Status hanya boleh PROCESSING, SHIPPED, atau DELIVERED.",
				[]httputil.FieldError{{Field: "status", Message: "tidak valid untuk fulfilment"}})
		}
		if !canMove(o.Status, status) {
			return "", invalidTransition(o.Status, status)
		}
		if status == model.StatusShipped {
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
	ids, err := s.repo.ExpiredPending(ctx, sqlitex.FormatTime(s.now()))
	if err != nil {
		return 0, err
	}
	sys := httputil.Identity{UserID: "SYSTEM", Role: httputil.RoleAdminSistem}
	ctx = httputil.WithIdentity(ctx, sys)
	n := 0
	for _, oid := range ids {
		o, err := s.repo.Apply(ctx, oid, "SYSTEM", func(o *model.Order) (string, error) {
			if o.Status != model.StatusPendingPayment {
				return "", invalidTransition(o.Status, model.StatusPaymentTimeout) // sudah berubah oleh request lain
			}
			markTimeout(o)
			return reasonTimeout, nil
		})
		if err != nil {
			slog.Warn("expire order dilewati", "order_id", oid, "err", err)
			continue
		}
		s.restockOrder(ctx, o, "PAYMENT_TIMEOUT")
		n++
	}
	return n, nil
}

// ---------- restock ----------

func (s *Service) restockOrder(ctx context.Context, o *model.Order, reason string) {
	items := make([]catalogclient.Item, len(o.Items))
	for i, it := range o.Items {
		items[i] = catalogclient.Item{ProductID: it.ProductID, Quantity: it.Quantity}
	}
	s.restock(ctx, o.ID, reason, items)
}

// restock memanggil catalog (idempoten, retry 2x di client). Kegagalan hanya di-log: status order sudah final.
func (s *Service) restock(ctx context.Context, orderID, reason string, items []catalogclient.Item) {
	if err := s.catalog.Restock(context.WithoutCancel(ctx), orderID, reason, items); err != nil {
		slog.Error("restock gagal — stok perlu direkonsiliasi", "order_id", orderID, "reason", reason, "err", err)
	}
}
