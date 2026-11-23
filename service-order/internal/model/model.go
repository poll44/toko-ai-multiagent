// Package model: tipe data service-order (JSON snake_case, TRD 03 §6).
package model

const (
	StatusPendingPayment = "PENDING_PAYMENT"
	StatusPaid           = "PAID"
	StatusProcessing     = "PROCESSING"
	StatusShipped        = "SHIPPED"
	StatusDelivered      = "DELIVERED"
	StatusCancelled      = "CANCELLED"
	StatusPaymentFailed  = "PAYMENT_FAILED"
	StatusPaymentTimeout = "PAYMENT_TIMEOUT"

	PayUnpaid   = "UNPAID"
	PayPaid     = "PAID"
	PayFailed   = "FAILED"
	PayExpired  = "EXPIRED"
	PayRefunded = "REFUNDED"
)

var Statuses = []string{
	StatusPendingPayment, StatusPaid, StatusProcessing, StatusShipped,
	StatusDelivered, StatusCancelled, StatusPaymentFailed, StatusPaymentTimeout,
}

type OrderItem struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	UnitPrice   int    `json:"unit_price"`
	Quantity    int    `json:"quantity"`
	Subtotal    int    `json:"subtotal"`
}

type HistoryEntry struct {
	FromStatus *string `json:"from_status"`
	ToStatus   string  `json:"to_status"`
	Note       *string `json:"note"`
	ActorID    string  `json:"actor_id"`
	CreatedAt  string  `json:"created_at"`
}

type Order struct {
	ID               string         `json:"id"`
	UserID           string         `json:"user_id"`
	Status           string         `json:"status"`
	PaymentStatus    string         `json:"payment_status"`
	PaymentMethod    string         `json:"payment_method"`
	TotalAmount      int            `json:"total_amount"`
	ShippingAddress  string         `json:"shipping_address"`
	TrackingNumber   *string        `json:"tracking_number"`
	FailureReason    *string        `json:"failure_reason"`
	PaymentExpiresAt *string        `json:"payment_expires_at"`
	PaidAt           *string        `json:"paid_at"`
	Items            []OrderItem    `json:"items"`
	History          []HistoryEntry `json:"history,omitempty"` // hanya pada detail
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

// ListFilter: UserID nil => semua pengguna (hanya untuk admin).
type ListFilter struct {
	UserID *string
	Status string
	Page   int
	Limit  int
}

func Ptr[T any](v T) *T { return &v }
