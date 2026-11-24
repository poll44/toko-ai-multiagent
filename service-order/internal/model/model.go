// Package model: tipe data service-order (JSON snake_case, TRD 03 §6).
package model // nama package: model

// Konstanta status pesanan & status pembayaran.
const (
	StatusPendingPayment = "PENDING_PAYMENT" // menunggu pembayaran
	StatusPaid           = "PAID"            // sudah dibayar
	StatusProcessing     = "PROCESSING"      // sedang diproses
	StatusShipped        = "SHIPPED"         // sudah dikirim
	StatusDelivered      = "DELIVERED"       // sudah diterima
	StatusCancelled      = "CANCELLED"       // dibatalkan
	StatusPaymentFailed  = "PAYMENT_FAILED"  // pembayaran gagal
	StatusPaymentTimeout = "PAYMENT_TIMEOUT" // pembayaran kedaluwarsa

	PayUnpaid   = "UNPAID"   // belum dibayar
	PayPaid     = "PAID"     // sudah dibayar
	PayFailed   = "FAILED"   // gagal
	PayExpired  = "EXPIRED"  // kedaluwarsa
	PayRefunded = "REFUNDED" // dikembalikan
)

// Statuses: daftar semua status pesanan yang valid.
var Statuses = []string{
	StatusPendingPayment, StatusPaid, StatusProcessing, StatusShipped,
	StatusDelivered, StatusCancelled, StatusPaymentFailed, StatusPaymentTimeout,
}

// OrderItem: satu baris barang pada pesanan (snapshot harga saat dipesan).
type OrderItem struct {
	ProductID   string `json:"product_id"`   // ID produk
	ProductName string `json:"product_name"` // nama produk saat dipesan
	UnitPrice   int    `json:"unit_price"`   // harga satuan saat dipesan
	Quantity    int    `json:"quantity"`     // jumlah
	Subtotal    int    `json:"subtotal"`     // harga x jumlah
}

// HistoryEntry: satu catatan perubahan status pesanan.
type HistoryEntry struct {
	FromStatus *string `json:"from_status"` // status sebelumnya (nil untuk awal)
	ToStatus   string  `json:"to_status"`   // status baru
	Note       *string `json:"note"`        // catatan (opsional)
	ActorID    string  `json:"actor_id"`    // pelaku perubahan
	CreatedAt  string  `json:"created_at"`  // waktu perubahan
}

// Order: data pesanan lengkap.
type Order struct {
	ID               string         `json:"id"`                 // ID pesanan, mis. ORD-001
	UserID           string         `json:"user_id"`            // pemilik pesanan
	Status           string         `json:"status"`             // status pesanan
	PaymentStatus    string         `json:"payment_status"`     // status pembayaran
	PaymentMethod    string         `json:"payment_method"`     // metode pembayaran
	TotalAmount      int            `json:"total_amount"`       // total harga
	ShippingAddress  string         `json:"shipping_address"`   // alamat pengiriman
	TrackingNumber   *string        `json:"tracking_number"`    // nomor resi
	FailureReason    *string        `json:"failure_reason"`     // alasan gagal bayar
	PaymentExpiresAt *string        `json:"payment_expires_at"` // batas waktu pembayaran
	PaidAt           *string        `json:"paid_at"`            // waktu dibayar
	Items            []OrderItem    `json:"items"`              // daftar barang
	History          []HistoryEntry `json:"history,omitempty"`  // hanya pada detail
	CreatedAt        string         `json:"created_at"`         // waktu dibuat
	UpdatedAt        string         `json:"updated_at"`         // waktu diubah
}

// ListFilter: UserID nil => semua pengguna (hanya untuk admin).
type ListFilter struct {
	UserID *string // filter pemilik
	Status string  // filter status
	Page   int     // nomor halaman
	Limit  int     // ukuran halaman
}

// Ptr mengembalikan pointer ke salinan nilai v (pembantu untuk field opsional).
func Ptr[T any](v T) *T { return &v }
