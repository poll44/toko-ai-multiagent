// Package service: logika tiket — validasi order, aturan lifecycle & otorisasi (TRD 04 §4, §6, §8).
package service // nama package: service

import ( // package yang dipakai
	"context" // context
	"errors"  // errors.As
	"slices"  // slices.Contains

	"toko-ai-microservices/internal/shared/httputil"            // identitas & error API
	"toko-ai-microservices/internal/shared/sqlitex"             // format waktu
	"toko-ai-microservices/internal/shared/svcclient"           // UpstreamError
	"toko-ai-microservices/service-ticket/internal/model"       // tipe data
	"toko-ai-microservices/service-ticket/internal/orderclient" // klien order
	"toko-ai-microservices/service-ticket/internal/store"       // repository
)

// OrderChecker diimplementasi *orderclient.Client (dan fake pada test).
type OrderChecker interface {
	GetOrder(ctx context.Context, id string) (*orderclient.Order, error) // ambil pesanan (validasi kepemilikan)
}

// Service berisi logika bisnis tiket.
type Service struct {
	repo   *store.Repo  // akses database
	orders OrderChecker // pengecek pesanan di service-order
}

// New membuat Service baru.
func New(repo *store.Repo, orders OrderChecker) *Service { return &Service{repo: repo, orders: orders} }

// CreateInput: data untuk membuat tiket.
type CreateInput struct {
	Subject, Description, Category, Priority string // field teks tiket
	OrderID                                  string // pesanan terkait (opsional)
}

// Create membuat tiket. user_id dari identitas; order_id divalidasi ke service-order dengan identitas ASLI user.
func (s *Service) Create(ctx context.Context, id httputil.Identity, in CreateInput) (*model.Ticket, error) {
	if in.OrderID != "" { // tiket terkait pesanan
		if _, err := s.orders.GetOrder(ctx, in.OrderID); err != nil { // validasi kepemilikan pesanan
			var ue *svcclient.UpstreamError
			if errors.As(err, &ue) {
				if ue.Status == 404 { // bukan milik user / tidak ada — tidak dibedakan
					e := httputil.ErrNotFound("Pesanan " + in.OrderID + " tidak ditemukan di akun Anda.")
					e.Details = map[string]any{"field": "order_id"}
					return nil, e
				}
				return nil, ue.APIError() // 502/504: tiket TIDAK dibuat
			}
			return nil, err
		}
		if existing, err := s.repo.ActiveForOrder(ctx, id.UserID, in.OrderID); err != nil { // cek tiket aktif untuk pesanan ini
			return nil, err
		} else if existing != "" { // sudah ada tiket aktif
			return nil, httputil.ErrConflict("CONFLICT", "Sudah ada tiket aktif untuk pesanan "+in.OrderID+".",
				map[string]any{"existing_ticket_id": existing})
		}
	}
	prio := in.Priority // prioritas yang diminta
	if prio == "" {     // default MEDIUM
		prio = "MEDIUM"
	}
	if in.Category == "TECHNICAL" && prio == "LOW" {
		prio = "MEDIUM" // keluhan teknis minimal MEDIUM
	}
	now := sqlitex.NowUTC() // waktu pembuatan
	t := &model.Ticket{
		UserID: id.UserID, Subject: in.Subject, Description: in.Description, Category: in.Category, Priority: prio,
		Status: model.StOpen, CreatedAt: now, UpdatedAt: now,
	}
	if in.OrderID != "" { // simpan pesanan terkait
		t.OrderID = model.Ptr(in.OrderID)
	}
	return s.repo.Create(ctx, t, id.Role) // simpan tiket
}

// authorize: CUSTOMER non-pemilik => 404; ADMIN_DEV hanya kategori TECHNICAL (selain itu 403).
func authorize(id httputil.Identity, t *model.Ticket) error {
	switch id.Role {
	case httputil.RoleCustomer: // pelanggan hanya tiket sendiri
		if t.UserID != id.UserID {
			return httputil.ErrNotFound("Tiket " + t.ID + " tidak ditemukan.")
		}
	case httputil.RoleAdminDev: // admin dev hanya TECHNICAL
		if t.Category != "TECHNICAL" {
			return httputil.ErrForbidden("ADMIN_DEV hanya dapat menangani tiket kategori TECHNICAL.")
		}
	}
	return nil
}

// Get: CUSTOMER non-pemilik => 404. Admin membaca semua (termasuk ADMIN_DEV: baca semua, tulis hanya TECHNICAL).
func (s *Service) Get(ctx context.Context, id httputil.Identity, ticketID string) (*model.Ticket, error) {
	t, err := s.repo.Get(ctx, ticketID, true) // ambil tiket + pesan + riwayat
	if err != nil {
		return nil, err
	}
	if id.Role == httputil.RoleCustomer && t.UserID != id.UserID { // bukan pemilik
		return nil, httputil.ErrNotFound("Tiket " + ticketID + " tidak ditemukan.")
	}
	return t, nil
}

// List meneruskan pencarian daftar tiket ke repository.
func (s *Service) List(ctx context.Context, f model.TicketFilter) ([]model.Ticket, int, error) {
	return s.repo.List(ctx, f)
}

// AddMessage menambah pesan; tiket CLOSED => 409.
func (s *Service) AddMessage(ctx context.Context, id httputil.Identity, ticketID, message string) (*model.Message, error) {
	t, err := s.repo.Apply(ctx, ticketID, id.UserID, id.Role, func(t *model.Ticket) (store.Change, error) {
		if err := authorize(id, t); err != nil { // cek hak akses
			return store.Change{}, err
		}
		if t.Status == model.StClosed { // tiket tertutup tidak bisa ditambah pesan
			return store.Change{}, invalid(t.Status, "pesan")
		}
		return store.Change{Message: message}, nil // simpan pesan bersama perubahan
	})
	if err != nil {
		return nil, err
	}
	last := t.Messages[len(t.Messages)-1] // pesan terakhir = pesan baru
	return &last, nil
}

// invalid membuat error 409 untuk transisi/aksi yang tidak sah pada status tertentu.
func invalid(from, to string) error {
	return httputil.ErrConflict("INVALID_STATE_TRANSITION", "Tiket berstatus "+from+" tidak dapat diubah ke/ditambah "+to+".",
		map[string]any{"from": from, "to": to})
}

// edge: satu transisi status (dari -> ke).
type edge struct{ from, to string }

// Alias role agar tabel transisi ringkas.
const (
	cust = httputil.RoleCustomer    // pelanggan
	adm  = httputil.RoleAdminSistem // admin sistem
	dev  = httputil.RoleAdminDev    // admin developer
)

// Siklus hidup & pelaku (TRD 04 §4). CLOSED final: tak ada edge keluar.
var edges = map[edge][]string{
	{model.StOpen, model.StInProgress}:     {adm, dev},       // mulai ditangani
	{model.StInProgress, model.StResolved}: {adm, dev},       // diselesaikan
	{model.StInProgress, model.StOpen}:     {adm, dev},       // dikembalikan ke antrean
	{model.StResolved, model.StOpen}:       {cust, adm, dev}, // dibuka kembali
	{model.StResolved, model.StClosed}:     {cust, adm, dev}, // ditutup setelah selesai
	{model.StOpen, model.StClosed}:         {cust, adm},      // ditutup langsung
}

// StatusInput: data untuk mengubah status tiket.
type StatusInput struct {
	Status, ResolutionNote, Message string // status tujuan, catatan penyelesaian, pesan
}

// fieldErr membuat error validasi untuk satu field.
func fieldErr(field, msg string) error {
	return httputil.ErrValidation(msg, []httputil.FieldError{{Field: field, Message: msg}})
}

// UpdateStatus menerapkan perubahan status: graf tidak sah => 409; pelaku tidak berhak => 403; syarat per target => 400.
func (s *Service) UpdateStatus(ctx context.Context, id httputil.Identity, ticketID string, in StatusInput) (*model.Ticket, error) {
	return s.repo.Apply(ctx, ticketID, id.UserID, id.Role, func(t *model.Ticket) (store.Change, error) {
		if err := authorize(id, t); err != nil { // cek hak akses
			return store.Change{}, err
		}
		from, to := t.Status, in.Status    // status asal & tujuan
		roles, ok := edges[edge{from, to}] // pelaku yang diizinkan untuk transisi ini
		if !ok {                           // transisi tidak ada di graf
			return store.Change{}, invalid(from, to)
		}
		if !slices.Contains(roles, id.Role) { // pelaku tidak berhak
			return store.Change{}, httputil.ErrForbidden("Anda tidak berhak mengubah status tiket dari " + from + " ke " + to + ".")
		}
		now := sqlitex.NowUTC() // waktu perubahan
		switch {
		case to == model.StInProgress: // mulai ditangani: tugaskan ke pelaku
			t.AssignedTo = model.Ptr(id.UserID)
		case to == model.StResolved: // selesai: catatan wajib
			if len([]rune(in.ResolutionNote)) < 10 {
				return store.Change{}, fieldErr("resolution_note", "resolution_note wajib diisi (minimal 10 karakter) saat RESOLVED")
			}
			t.ResolutionNote, t.ResolvedAt = model.Ptr(in.ResolutionNote), model.Ptr(now)
		case from == model.StInProgress && to == model.StOpen: // kembali ke antrean: lepas penugasan
			t.AssignedTo = nil
		case from == model.StResolved && to == model.StOpen: // reopen
			if in.Message == "" { // alasan wajib
				return store.Change{}, fieldErr("message", "message (alasan membuka kembali) wajib diisi")
			}
			t.ResolvedAt = nil
		case to == model.StClosed: // ditutup
			t.ClosedAt = model.Ptr(now)
		}
		t.Status = to                                 // terapkan status baru
		return store.Change{Message: in.Message}, nil // pesan (bila ada) disimpan atomik
	})
}
