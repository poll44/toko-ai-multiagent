// Package model: tipe data service-catalog (JSON snake_case, sesuai TRD 02 §6).
package model // nama package: model

// Product adalah satu produk di katalog.
type Product struct {
	ID             string  `json:"id"`             // ID produk, mis. PRD-001
	Name           string  `json:"name"`           // nama produk
	Description    string  `json:"description"`    // deskripsi
	Specifications string  `json:"specifications"` // spesifikasi teknis
	Category       string  `json:"category"`       // kategori
	Price          int     `json:"price"`          // harga (Rupiah)
	Stock          int     `json:"stock"`          // jumlah stok
	StockStatus    string  `json:"stock_status"`   // OUT_OF_STOCK / LOW_STOCK / IN_STOCK
	ImageURL       *string `json:"image_url"`      // URL gambar (opsional)
	IsActive       bool    `json:"is_active"`      // produk aktif dijual?
	CreatedAt      string  `json:"created_at"`     // waktu dibuat (RFC 3339)
	UpdatedAt      string  `json:"updated_at"`     // waktu diubah (RFC 3339)
}

// StockStatusOf: OUT_OF_STOCK (0), LOW_STOCK (1–10), IN_STOCK (>10).
func StockStatusOf(stock int) string {
	switch {
	case stock <= 0: // habis
		return "OUT_OF_STOCK"
	case stock <= 10: // menipis
		return "LOW_STOCK"
	default: // tersedia
		return "IN_STOCK"
	}
}

// ScoredProduct: hasil pencarian semantik; Score null pada keyword_fallback.
type ScoredProduct struct {
	Product          // data produk (di-embed)
	Score   *float64 `json:"score"` // skor kemiripan (nil pada fallback keyword)
}

// Category: ringkasan satu kategori beserta jumlah produknya.
type Category struct {
	Category string `json:"category"` // nama kategori
	Count    int    `json:"count"`    // jumlah produk
}

// StockMovement: catatan satu perubahan stok (audit trail).
type StockMovement struct {
	ID          int64   `json:"id"`           // ID urut
	ProductID   string  `json:"product_id"`   // produk terkait
	Delta       int     `json:"delta"`        // perubahan stok (+/-)
	StockAfter  int     `json:"stock_after"`  // stok setelah perubahan
	Reason      string  `json:"reason"`       // alasan perubahan
	ReferenceID *string `json:"reference_id"` // referensi (mis. ID pesanan)
	ActorID     string  `json:"actor_id"`     // pelaku perubahan
	CreatedAt   string  `json:"created_at"`   // waktu perubahan
}

// ProductInput: data tulis produk (stock hanya dipakai saat create).
type ProductInput struct {
	Name           string  // nama produk
	Description    string  // deskripsi
	Specifications string  // spesifikasi
	Category       string  // kategori
	Price          int     // harga
	Stock          int     // stok awal (hanya saat create)
	ImageURL       *string // URL gambar
	IsActive       bool    // status aktif
}

// ListFilter: filter pencarian eksak & fallback keyword.
type ListFilter struct {
	Q               string // kata kunci
	Category        string // filter kategori
	MinPrice        *int   // harga minimum
	MaxPrice        *int   // harga maksimum
	InStock         *bool  // hanya yang ada stok
	Sort            string // urutan hasil
	Page            int    // nomor halaman
	Limit           int    // ukuran halaman
	IncludeInactive bool   // sertakan produk nonaktif
}

// StockItem: pasangan produk & jumlah untuk operasi stok.
type StockItem struct {
	ProductID string `json:"product_id"` // ID produk
	Quantity  int    `json:"quantity"`   // jumlah
}

// ReservedItem: hasil reservasi stok satu item (snapshot harga & stok).
type ReservedItem struct {
	ProductID  string `json:"product_id"`  // ID produk
	Name       string `json:"name"`        // nama produk saat dipesan
	UnitPrice  int    `json:"unit_price"`  // harga satuan saat dipesan
	Quantity   int    `json:"quantity"`    // jumlah dipesan
	StockAfter int    `json:"stock_after"` // stok tersisa
}
