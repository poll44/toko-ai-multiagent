// Package handlers: routing + guard RBAC + handler service-ticket (TRD 04 §2, §6, §7).
package handlers

import (
	"net/http"
	"regexp"

	h "toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/service-ticket/internal/knowledge"
	"toko-ai-microservices/service-ticket/internal/service"
	"toko-ai-microservices/service-ticket/internal/store"
)

type Deps struct {
	Svc      *service.Service
	Repo     *store.Repo
	Index    *knowledge.Indexer
	MinScore float64 // SEMANTIC_MIN_SCORE
}

var (
	ticketIDRe = regexp.MustCompile(`^TKT-\d{3,}$`)
	docIDRe    = regexp.MustCompile(`^KB-\d{3,}$`)
	orderIDRe  = regexp.MustCompile(`^ORD-\d{3,}$`)
	userIDRe   = regexp.MustCompile(`^(USR|ADM|DEV)-\d{2,}$`)
)

func Register(mux *http.ServeMux, internalKey string, d *Deps) {
	secured := h.Chain(h.InternalAuthMiddleware(internalKey), h.IdentityMiddleware()) // GUEST lolos (knowledge publik)
	authed := h.Chain(secured, h.DenyGuest)                                           // tiket: GUEST ditolak
	customer := h.RequireRole(h.RoleCustomer)
	admin := h.RequireRole(h.RoleAdminSistem)

	mux.HandleFunc("GET /health", h.Health("service-ticket"))

	// Tiket
	mux.Handle("POST /api/tickets", authed(customer(http.HandlerFunc(d.createTicket))))
	mux.Handle("GET /api/tickets", authed(http.HandlerFunc(d.listTickets)))
	mux.Handle("GET /api/tickets/{id}", authed(http.HandlerFunc(d.getTicket)))
	mux.Handle("POST /api/tickets/{id}/messages", authed(http.HandlerFunc(d.addMessage)))
	mux.Handle("PATCH /api/tickets/{id}/status", authed(http.HandlerFunc(d.updateStatus)))

	// Knowledge base (RAG)
	mux.Handle("GET /api/knowledge/search", secured(http.HandlerFunc(d.searchKnowledge)))
	mux.Handle("GET /api/knowledge/documents", secured(http.HandlerFunc(d.listDocs)))
	mux.Handle("GET /api/knowledge/documents/{id}", secured(http.HandlerFunc(d.getDoc)))
	mux.Handle("POST /api/knowledge/documents", secured(admin(http.HandlerFunc(d.createDoc))))
	mux.Handle("PUT /api/knowledge/documents/{id}", secured(admin(http.HandlerFunc(d.updateDoc))))
	mux.Handle("DELETE /api/knowledge/documents/{id}", secured(admin(http.HandlerFunc(d.deleteDoc))))
	mux.Handle("POST /api/knowledge/reindex", secured(admin(http.HandlerFunc(d.reindex))))
}

func ident(r *http.Request) h.Identity { return h.IdentityFrom(r.Context()) }
