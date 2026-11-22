// Package model: tipe data service-catalog (JSON snake_case, sesuai PRD 02 §6).
package model

type Product struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Specifications string  `json:"specifications"`
	Category       string  `json:"category"`
	Price          int     `json:"price"`
	Stock          int     `json:"stock"`
	StockStatus    string  `json:"stock_status"`
	ImageURL       *string `json:"image_url"`
	IsActive       bool    `json:"is_active"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// StockStatusOf: OUT_OF_STOCK (0), LOW_STOCK (1–10), IN_STOCK (>10).
func StockStatusOf(stock int) string {
	switch {
	case stock <= 0:
		return "OUT_OF_STOCK"
	case stock <= 10:
		return "LOW_STOCK"
	default:
		return "IN_STOCK"
	}
}

// ScoredProduct: hasil pencarian semantik; Score null pada keyword_fallback.
type ScoredProduct struct {
	Product
	Score *float64 `json:"score"`
}

type Category struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type StockMovement struct {
	ID          int64   `json:"id"`
	ProductID   string  `json:"product_id"`
	Delta       int     `json:"delta"`
	StockAfter  int     `json:"stock_after"`
	Reason      string  `json:"reason"`
	ReferenceID *string `json:"reference_id"`
	ActorID     string  `json:"actor_id"`
	CreatedAt   string  `json:"created_at"`
}

// ProductInput: data tulis produk (stock hanya dipakai saat create).
type ProductInput struct {
	Name           string
	Description    string
	Specifications string
	Category       string
	Price          int
	Stock          int
	ImageURL       *string
	IsActive       bool
}

// ListFilter: filter pencarian eksak & fallback keyword.
type ListFilter struct {
	Q               string
	Category        string
	MinPrice        *int
	MaxPrice        *int
	InStock         *bool
	Sort            string
	Page            int
	Limit           int
	IncludeInactive bool
}

type StockItem struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type ReservedItem struct {
	ProductID  string `json:"product_id"`
	Name       string `json:"name"`
	UnitPrice  int    `json:"unit_price"`
	Quantity   int    `json:"quantity"`
	StockAfter int    `json:"stock_after"`
}
