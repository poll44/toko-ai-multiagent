// Package handlers: routing + guard RBAC + handler service-catalog (TRD 02 §2, §6).
package handlers // nama package: handlers

import ( // package yang dipakai
	"net/http" // server HTTP

	h "toko-ai-microservices/internal/shared/httputil"        // helper HTTP bersama (alias h)
	"toko-ai-microservices/service-catalog/internal/semantic" // indexer semantik
	"toko-ai-microservices/service-catalog/internal/store"    // repository
)

// Deps: dependensi handler.
type Deps struct {
	Repo     *store.Repo       // akses data produk
	Index    *semantic.Indexer // indexer & pencarian semantik
	MinScore float64           // SEMANTIC_MIN_SCORE
}

// Register mendaftarkan semua rute service-catalog beserta guard RBAC-nya ke mux.
func Register(mux *http.ServeMux, internalKey string, d *Deps) {
	secured := h.Chain(h.InternalAuthMiddleware(internalKey), h.IdentityMiddleware()) // wajib kunci internal + identitas
	admin := h.RequireRole(h.RoleAdminSistem)                                         // hanya ADMIN_SISTEM
	adminRead := h.RequireRole(h.RoleAdminSistem, h.RoleAdminDev)                     // admin sistem atau dev (baca)
	orderOnly := h.RequireServiceCaller("service-order")                              // hanya dipanggil service-order

	mux.HandleFunc("GET /health", h.Health("service-catalog")) // tanpa internal key

	mux.Handle("GET /api/products", secured(http.HandlerFunc(d.listProducts)))                                   // daftar/cari produk
	mux.Handle("GET /api/products/categories", secured(http.HandlerFunc(d.categories)))                          // daftar kategori
	mux.Handle("GET /api/products/semantic-search", secured(http.HandlerFunc(d.semanticSearch)))                 // pencarian semantik
	mux.Handle("GET /api/products/{id}", secured(http.HandlerFunc(d.getProduct)))                                // detail produk
	mux.Handle("POST /api/products", secured(admin(http.HandlerFunc(d.createProduct))))                          // buat produk
	mux.Handle("PUT /api/products/{id}", secured(admin(http.HandlerFunc(d.updateProduct))))                      // ubah produk
	mux.Handle("DELETE /api/products/{id}", secured(admin(http.HandlerFunc(d.deleteProduct))))                   // hapus/nonaktifkan produk
	mux.Handle("PATCH /api/products/{id}/stock", secured(admin(http.HandlerFunc(d.adjustStock))))                // sesuaikan stok
	mux.Handle("GET /api/products/{id}/stock-movements", secured(adminRead(http.HandlerFunc(d.stockMovements)))) // riwayat stok
	mux.Handle("POST /api/products/reindex", secured(admin(http.HandlerFunc(d.reindex))))                        // indeks ulang embedding

	mux.Handle("POST /internal/stock/decrement", secured(orderOnly(http.HandlerFunc(d.decrement)))) // kurangi stok (service-order)
	mux.Handle("POST /internal/stock/restock", secured(orderOnly(http.HandlerFunc(d.restock))))     // kembalikan stok (service-order)
}
