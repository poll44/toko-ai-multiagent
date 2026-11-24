// Package handlers: routing + guard RBAC + handler service-ticket (TRD 04 §2, §6, §7).
package handlers // nama package: handlers

import ( // package yang dipakai
	"net/http" // tipe HTTP
	"regexp"   // regular expression

	h "toko-ai-microservices/internal/shared/httputil"        // helper HTTP bersama (alias h)
	"toko-ai-microservices/service-ticket/internal/knowledge" // indexer knowledge base
	"toko-ai-microservices/service-ticket/internal/service"   // logika bisnis tiket
	"toko-ai-microservices/service-ticket/internal/store"     // repository
)

// Deps: dependensi handler.
type Deps struct {
	Svc      *service.Service   // logika bisnis tiket
	Repo     *store.Repo        // akses data (knowledge base)
	Index    *knowledge.Indexer // indexer & pencarian KB
	MinScore float64            // SEMANTIC_MIN_SCORE
}

var (
	ticketIDRe = regexp.MustCompile(`^TKT-\d{3,}$`)           // format ID tiket
	docIDRe    = regexp.MustCompile(`^KB-\d{3,}$`)            // format ID dokumen KB
	orderIDRe  = regexp.MustCompile(`^ORD-\d{3,}$`)           // format ID pesanan
	userIDRe   = regexp.MustCompile(`^(USR|ADM|DEV)-\d{2,}$`) // format ID user
)

// Register mendaftarkan semua rute service-ticket beserta guard RBAC-nya ke mux.
func Register(mux *http.ServeMux, internalKey string, d *Deps) {
	secured := h.Chain(h.InternalAuthMiddleware(internalKey), h.IdentityMiddleware()) // GUEST lolos (knowledge publik)
	authed := h.Chain(secured, h.DenyGuest)                                           // tiket: GUEST ditolak
	customer := h.RequireRole(h.RoleCustomer)                                         // hanya CUSTOMER
	admin := h.RequireRole(h.RoleAdminSistem)                                         // hanya ADMIN_SISTEM

	mux.HandleFunc("GET /health", h.Health("service-ticket")) // health check tanpa auth

	// Tiket
	mux.Handle("POST /api/tickets", authed(customer(http.HandlerFunc(d.createTicket))))    // buat tiket
	mux.Handle("GET /api/tickets", authed(http.HandlerFunc(d.listTickets)))                // daftar tiket
	mux.Handle("GET /api/tickets/{id}", authed(http.HandlerFunc(d.getTicket)))             // detail tiket
	mux.Handle("POST /api/tickets/{id}/messages", authed(http.HandlerFunc(d.addMessage)))  // tambah pesan
	mux.Handle("PATCH /api/tickets/{id}/status", authed(http.HandlerFunc(d.updateStatus))) // ubah status

	// Knowledge base (RAG)
	mux.Handle("GET /api/knowledge/search", secured(http.HandlerFunc(d.searchKnowledge)))             // cari kebijakan
	mux.Handle("GET /api/knowledge/documents", secured(http.HandlerFunc(d.listDocs)))                 // daftar dokumen
	mux.Handle("GET /api/knowledge/documents/{id}", secured(http.HandlerFunc(d.getDoc)))              // detail dokumen
	mux.Handle("POST /api/knowledge/documents", secured(admin(http.HandlerFunc(d.createDoc))))        // buat dokumen
	mux.Handle("PUT /api/knowledge/documents/{id}", secured(admin(http.HandlerFunc(d.updateDoc))))    // ubah dokumen
	mux.Handle("DELETE /api/knowledge/documents/{id}", secured(admin(http.HandlerFunc(d.deleteDoc)))) // nonaktifkan dokumen
	mux.Handle("POST /api/knowledge/reindex", secured(admin(http.HandlerFunc(d.reindex))))            // indeks ulang embedding
}

// ident mengambil identitas pemanggil dari context request.
func ident(r *http.Request) h.Identity { return h.IdentityFrom(r.Context()) }
