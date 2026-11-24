// Package catalogclient: HTTP client native ke service-catalog (TRD 03 §6.2).
package catalogclient // nama package: catalogclient

import ( // package yang dipakai
	"context"  // context
	"net/http" // konstanta metode HTTP
	"time"     // durasi

	"toko-ai-microservices/internal/shared/svcclient" // klien service-ke-service
)

// Item: pasangan produk & jumlah yang dikirim ke catalog.
type Item struct {
	ProductID string `json:"product_id"` // ID produk
	Quantity  int    `json:"quantity"`   // jumlah
}

// ReservedItem: hasil reservasi stok dari catalog (snapshot harga & nama).
type ReservedItem struct {
	ProductID  string `json:"product_id"`  // ID produk
	Name       string `json:"name"`        // nama produk
	UnitPrice  int    `json:"unit_price"`  // harga satuan
	Quantity   int    `json:"quantity"`    // jumlah
	StockAfter int    `json:"stock_after"` // stok tersisa
}

// Client membungkus svcclient untuk memanggil service-catalog.
type Client struct{ c *svcclient.Client }

// New membuat klien catalog dengan identitas pemanggil "service-order".
func New(baseURL, internalKey string, timeout time.Duration) *Client {
	return &Client{c: svcclient.New(baseURL, internalKey, "service-order", timeout)}
}

// Decrement: kurangi stok atomik; TANPA retry (hindari potong ganda). ctx membawa identitas user asal.
func (cl *Client) Decrement(ctx context.Context, referenceID string, items []Item) ([]ReservedItem, error) {
	var out struct { // penampung respons
		Items []ReservedItem `json:"items"`
	}
	err := cl.c.Do(ctx, http.MethodPost, "/internal/stock/decrement", // panggil endpoint internal catalog
		map[string]any{"reference_id": referenceID, "items": items}, &out)
	return out.Items, err
}

// Restock: kembalikan stok; idempoten di catalog sehingga aman di-retry (maks 2x).
func (cl *Client) Restock(ctx context.Context, referenceID, reason string, items []Item) error {
	return cl.c.DoWithRetry(ctx, 2, 200*time.Millisecond, http.MethodPost, "/internal/stock/restock", // retry 2x dengan backoff 200ms
		map[string]any{"reference_id": referenceID, "reason": reason, "items": items}, nil)
}
