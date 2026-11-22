// Package catalogclient: HTTP client native ke service-catalog (PRD 03 §6.2).
package catalogclient

import (
	"context"
	"net/http"
	"time"

	"toko-ai-microservices/internal/shared/svcclient"
)

type Item struct {
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

type Client struct{ c *svcclient.Client }

func New(baseURL, internalKey string, timeout time.Duration) *Client {
	return &Client{c: svcclient.New(baseURL, internalKey, "service-order", timeout)}
}

// Decrement: kurangi stok atomik; TANPA retry (hindari potong ganda). ctx membawa identitas user asal.
func (cl *Client) Decrement(ctx context.Context, referenceID string, items []Item) ([]ReservedItem, error) {
	var out struct {
		Items []ReservedItem `json:"items"`
	}
	err := cl.c.Do(ctx, http.MethodPost, "/internal/stock/decrement",
		map[string]any{"reference_id": referenceID, "items": items}, &out)
	return out.Items, err
}

// Restock: kembalikan stok; idempoten di catalog sehingga aman di-retry (maks 2x).
func (cl *Client) Restock(ctx context.Context, referenceID, reason string, items []Item) error {
	return cl.c.DoWithRetry(ctx, 2, 200*time.Millisecond, http.MethodPost, "/internal/stock/restock",
		map[string]any{"reference_id": referenceID, "reason": reason, "items": items}, nil)
}
