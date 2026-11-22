// Package service: logika tiket — validasi order, aturan lifecycle & otorisasi (PRD 04 §4, §6, §8).
package service

import (
	"context"
	"errors"
	"slices"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/sqlitex"
	"toko-ai-microservices/internal/shared/svcclient"
	"toko-ai-microservices/service-ticket/internal/model"
	"toko-ai-microservices/service-ticket/internal/orderclient"
	"toko-ai-microservices/service-ticket/internal/store"
)

// OrderChecker diimplementasi *orderclient.Client (dan fake pada test).
type OrderChecker interface {
	GetOrder(ctx context.Context, id string) (*orderclient.Order, error)
}

type Service struct {
	repo   *store.Repo
	orders OrderChecker
}

func New(repo *store.Repo, orders OrderChecker) *Service { return &Service{repo: repo, orders: orders} }

type CreateInput struct {
	Subject, Description, Category, Priority string
	OrderID                                  string
}

// Create membuat tiket. user_id dari identitas; order_id divalidasi ke service-order dengan identitas ASLI user.
func (s *Service) Create(ctx context.Context, id httputil.Identity, in CreateInput) (*model.Ticket, error) {
	if in.OrderID != "" {
		if _, err := s.orders.GetOrder(ctx, in.OrderID); err != nil {
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
		if existing, err := s.repo.ActiveForOrder(ctx, id.UserID, in.OrderID); err != nil {
			return nil, err
		} else if existing != "" {
			return nil, httputil.ErrConflict("CONFLICT", "Sudah ada tiket aktif untuk pesanan "+in.OrderID+".",
				map[string]any{"existing_ticket_id": existing})
		}
	}
	prio := in.Priority
	if prio == "" {
		prio = "MEDIUM"
	}
	if in.Category == "TECHNICAL" && prio == "LOW" {
		prio = "MEDIUM" // keluhan teknis minimal MEDIUM
	}
	now := sqlitex.NowUTC()
	t := &model.Ticket{
		UserID: id.UserID, Subject: in.Subject, Description: in.Description, Category: in.Category, Priority: prio,
		Status: model.StOpen, CreatedAt: now, UpdatedAt: now,
	}
	if in.OrderID != "" {
		t.OrderID = model.Ptr(in.OrderID)
	}
	return s.repo.Create(ctx, t, id.Role)
}

// authorize: CUSTOMER non-pemilik => 404; ADMIN_DEV hanya kategori TECHNICAL (selain itu 403).
func authorize(id httputil.Identity, t *model.Ticket) error {
	switch id.Role {
	case httputil.RoleCustomer:
		if t.UserID != id.UserID {
			return httputil.ErrNotFound("Tiket " + t.ID + " tidak ditemukan.")
		}
	case httputil.RoleAdminDev:
		if t.Category != "TECHNICAL" {
			return httputil.ErrForbidden("ADMIN_DEV hanya dapat menangani tiket kategori TECHNICAL.")
		}
	}
	return nil
}

// Get: CUSTOMER non-pemilik => 404. Admin membaca semua (termasuk ADMIN_DEV: baca semua, tulis hanya TECHNICAL).
func (s *Service) Get(ctx context.Context, id httputil.Identity, ticketID string) (*model.Ticket, error) {
	t, err := s.repo.Get(ctx, ticketID, true)
	if err != nil {
		return nil, err
	}
	if id.Role == httputil.RoleCustomer && t.UserID != id.UserID {
		return nil, httputil.ErrNotFound("Tiket " + ticketID + " tidak ditemukan.")
	}
	return t, nil
}

func (s *Service) List(ctx context.Context, f model.TicketFilter) ([]model.Ticket, int, error) {
	return s.repo.List(ctx, f)
}

// AddMessage menambah pesan; tiket CLOSED => 409.
func (s *Service) AddMessage(ctx context.Context, id httputil.Identity, ticketID, message string) (*model.Message, error) {
	t, err := s.repo.Apply(ctx, ticketID, id.UserID, id.Role, func(t *model.Ticket) (store.Change, error) {
		if err := authorize(id, t); err != nil {
			return store.Change{}, err
		}
		if t.Status == model.StClosed {
			return store.Change{}, invalid(t.Status, "pesan")
		}
		return store.Change{Message: message}, nil
	})
	if err != nil {
		return nil, err
	}
	last := t.Messages[len(t.Messages)-1]
	return &last, nil
}

func invalid(from, to string) error {
	return httputil.ErrConflict("INVALID_STATE_TRANSITION", "Tiket berstatus "+from+" tidak dapat diubah ke/ditambah "+to+".",
		map[string]any{"from": from, "to": to})
}

type edge struct{ from, to string }

const (
	cust = httputil.RoleCustomer
	adm  = httputil.RoleAdminSistem
	dev  = httputil.RoleAdminDev
)

// Siklus hidup & pelaku (PRD 04 §4). CLOSED final: tak ada edge keluar.
var edges = map[edge][]string{
	{model.StOpen, model.StInProgress}:     {adm, dev},
	{model.StInProgress, model.StResolved}: {adm, dev},
	{model.StInProgress, model.StOpen}:     {adm, dev},
	{model.StResolved, model.StOpen}:       {cust, adm, dev},
	{model.StResolved, model.StClosed}:     {cust, adm, dev},
	{model.StOpen, model.StClosed}:         {cust, adm},
}

type StatusInput struct {
	Status, ResolutionNote, Message string
}

func fieldErr(field, msg string) error {
	return httputil.ErrValidation(msg, []httputil.FieldError{{Field: field, Message: msg}})
}

// UpdateStatus menerapkan perubahan status: graf tidak sah => 409; pelaku tidak berhak => 403; syarat per target => 400.
func (s *Service) UpdateStatus(ctx context.Context, id httputil.Identity, ticketID string, in StatusInput) (*model.Ticket, error) {
	return s.repo.Apply(ctx, ticketID, id.UserID, id.Role, func(t *model.Ticket) (store.Change, error) {
		if err := authorize(id, t); err != nil {
			return store.Change{}, err
		}
		from, to := t.Status, in.Status
		roles, ok := edges[edge{from, to}]
		if !ok {
			return store.Change{}, invalid(from, to)
		}
		if !slices.Contains(roles, id.Role) {
			return store.Change{}, httputil.ErrForbidden("Anda tidak berhak mengubah status tiket dari " + from + " ke " + to + ".")
		}
		now := sqlitex.NowUTC()
		switch {
		case to == model.StInProgress:
			t.AssignedTo = model.Ptr(id.UserID)
		case to == model.StResolved:
			if len([]rune(in.ResolutionNote)) < 10 {
				return store.Change{}, fieldErr("resolution_note", "resolution_note wajib diisi (minimal 10 karakter) saat RESOLVED")
			}
			t.ResolutionNote, t.ResolvedAt = model.Ptr(in.ResolutionNote), model.Ptr(now)
		case from == model.StInProgress && to == model.StOpen:
			t.AssignedTo = nil
		case from == model.StResolved && to == model.StOpen: // reopen
			if in.Message == "" {
				return store.Change{}, fieldErr("message", "message (alasan membuka kembali) wajib diisi")
			}
			t.ResolvedAt = nil
		case to == model.StClosed:
			t.ClosedAt = model.Ptr(now)
		}
		t.Status = to
		return store.Change{Message: in.Message}, nil
	})
}
