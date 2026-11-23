// Package orderclient: validasi kepemilikan order ke service-order (TRD 04 §6.1).
package orderclient

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"toko-ai-microservices/internal/shared/svcclient"
)

// Order: subset field yang dibutuhkan service-ticket.
type Order struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	Status string `json:"status"`
}

type Client struct{ c *svcclient.Client }

func New(baseURL, internalKey string, timeout time.Duration) *Client {
	return &Client{c: svcclient.New(baseURL, internalKey, "service-ticket", timeout)}
}

// GetOrder memanggil GET /api/orders/{id} dengan identitas ASLI user (dari ctx); non-pemilik => 404 dari order.
func (cl *Client) GetOrder(ctx context.Context, id string) (*Order, error) {
	var o Order
	if err := cl.c.Do(ctx, http.MethodGet, "/api/orders/"+url.PathEscape(id), nil, &o); err != nil {
		return nil, err
	}
	return &o, nil
}
