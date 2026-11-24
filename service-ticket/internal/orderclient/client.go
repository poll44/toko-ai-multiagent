// Package orderclient: validasi kepemilikan order ke service-order (TRD 04 §6.1).
package orderclient // nama package: orderclient

import ( // package yang dipakai
	"context"  // context
	"net/http" // konstanta metode HTTP
	"net/url"  // escape path
	"time"     // durasi

	"toko-ai-microservices/internal/shared/svcclient" // klien service-ke-service
)

// Order: subset field yang dibutuhkan service-ticket.
type Order struct {
	ID     string `json:"id"`      // ID pesanan
	UserID string `json:"user_id"` // pemilik pesanan
	Status string `json:"status"`  // status pesanan
}

// Client membungkus svcclient untuk memanggil service-order.
type Client struct{ c *svcclient.Client }

// New membuat klien order dengan identitas pemanggil "service-ticket".
func New(baseURL, internalKey string, timeout time.Duration) *Client {
	return &Client{c: svcclient.New(baseURL, internalKey, "service-ticket", timeout)}
}

// GetOrder memanggil GET /api/orders/{id} dengan identitas ASLI user (dari ctx); non-pemilik => 404 dari order.
func (cl *Client) GetOrder(ctx context.Context, id string) (*Order, error) {
	var o Order                                                                                      // penampung respons
	if err := cl.c.Do(ctx, http.MethodGet, "/api/orders/"+url.PathEscape(id), nil, &o); err != nil { // ID di-escape agar aman
		return nil, err
	}
	return &o, nil
}
