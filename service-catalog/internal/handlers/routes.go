// Package handlers: routing + guard RBAC + handler service-catalog (TRD 02 §2, §6).
package handlers

import (
	"net/http"

	h "toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/service-catalog/internal/semantic"
	"toko-ai-microservices/service-catalog/internal/store"
)

// Deps: dependensi handler.
type Deps struct {
	Repo     *store.Repo
	Index    *semantic.Indexer
	MinScore float64 // SEMANTIC_MIN_SCORE
}

func Register(mux *http.ServeMux, internalKey string, d *Deps) {
	secured := h.Chain(h.InternalAuthMiddleware(internalKey), h.IdentityMiddleware())
	admin := h.RequireRole(h.RoleAdminSistem)
	adminRead := h.RequireRole(h.RoleAdminSistem, h.RoleAdminDev)
	orderOnly := h.RequireServiceCaller("service-order")

	mux.HandleFunc("GET /health", h.Health("service-catalog")) // tanpa internal key

	mux.Handle("GET /api/products", secured(http.HandlerFunc(d.listProducts)))
	mux.Handle("GET /api/products/categories", secured(http.HandlerFunc(d.categories)))
	mux.Handle("GET /api/products/semantic-search", secured(http.HandlerFunc(d.semanticSearch)))
	mux.Handle("GET /api/products/{id}", secured(http.HandlerFunc(d.getProduct)))
	mux.Handle("POST /api/products", secured(admin(http.HandlerFunc(d.createProduct))))
	mux.Handle("PUT /api/products/{id}", secured(admin(http.HandlerFunc(d.updateProduct))))
	mux.Handle("DELETE /api/products/{id}", secured(admin(http.HandlerFunc(d.deleteProduct))))
	mux.Handle("PATCH /api/products/{id}/stock", secured(admin(http.HandlerFunc(d.adjustStock))))
	mux.Handle("GET /api/products/{id}/stock-movements", secured(adminRead(http.HandlerFunc(d.stockMovements))))
	mux.Handle("POST /api/products/reindex", secured(admin(http.HandlerFunc(d.reindex))))

	mux.Handle("POST /internal/stock/decrement", secured(orderOnly(http.HandlerFunc(d.decrement))))
	mux.Handle("POST /internal/stock/restock", secured(orderOnly(http.HandlerFunc(d.restock))))
}
