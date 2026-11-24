// Package model: tipe data service-ticket (JSON snake_case, TRD 04 §6–§7).
package model // nama package: model

// Status tiket.
const (
	StOpen       = "OPEN"        // baru dibuat
	StInProgress = "IN_PROGRESS" // sedang ditangani
	StResolved   = "RESOLVED"    // sudah diselesaikan
	StClosed     = "CLOSED"      // ditutup
)

var (
	Categories   = []string{"PAYMENT", "SHIPPING", "PRODUCT", "TECHNICAL", "OTHER"}                       // kategori tiket
	Priorities   = []string{"LOW", "MEDIUM", "HIGH"}                                                      // prioritas tiket
	Statuses     = []string{StOpen, StInProgress, StResolved, StClosed}                                   // semua status tiket
	KBCategories = []string{"RETURN", "WARRANTY", "SHIPPING", "PAYMENT", "ORDER", "COMPLAINT", "GENERAL"} // kategori dokumen KB
)

// Message: satu pesan dalam percakapan tiket.
type Message struct {
	ID         int64  `json:"id"`          // ID urut
	AuthorID   string `json:"author_id"`   // penulis pesan
	AuthorRole string `json:"author_role"` // role penulis
	Message    string `json:"message"`     // isi pesan
	CreatedAt  string `json:"created_at"`  // waktu dikirim
}

// History: satu catatan perubahan status tiket.
type History struct {
	FromStatus *string `json:"from_status"` // status sebelumnya (nil untuk awal)
	ToStatus   string  `json:"to_status"`   // status baru
	ActorID    string  `json:"actor_id"`    // pelaku perubahan
	CreatedAt  string  `json:"created_at"`  // waktu perubahan
}

// Ticket: data tiket komplain lengkap.
type Ticket struct {
	ID             string    `json:"id"`                 // ID tiket, mis. TKT-001
	UserID         string    `json:"user_id"`            // pemilik tiket
	OrderID        *string   `json:"order_id"`           // pesanan terkait (opsional)
	Subject        string    `json:"subject"`            // subjek
	Description    string    `json:"description"`        // deskripsi masalah
	Category       string    `json:"category"`           // kategori
	Priority       string    `json:"priority"`           // prioritas
	Status         string    `json:"status"`             // status
	AssignedTo     *string   `json:"assigned_to"`        // admin penanggung jawab
	ResolutionNote *string   `json:"resolution_note"`    // catatan penyelesaian
	CreatedAt      string    `json:"created_at"`         // waktu dibuat
	UpdatedAt      string    `json:"updated_at"`         // waktu diubah
	ResolvedAt     *string   `json:"resolved_at"`        // waktu diselesaikan
	ClosedAt       *string   `json:"closed_at"`          // waktu ditutup
	Messages       []Message `json:"messages,omitempty"` // hanya detail
	History        []History `json:"history,omitempty"`  // hanya detail
}

// TicketFilter: filter pencarian daftar tiket.
type TicketFilter struct {
	UserID     *string // filter pemilik (nil = semua, hanya admin)
	Status     string  // filter status
	Category   string  // filter kategori
	Priority   string  // filter prioritas
	OrderID    string  // filter pesanan terkait
	AssignedTo string  // filter penanggung jawab
	Page       int     // nomor halaman
	Limit      int     // ukuran halaman
}

// ---- Knowledge base ----

// Document: dokumen kebijakan pada knowledge base.
type Document struct {
	ID         string  `json:"id"`                // ID dokumen, mis. KB-001
	Title      string  `json:"title"`             // judul
	Category   string  `json:"category"`          // kategori KB
	Content    *string `json:"content,omitempty"` // tidak dikirim pada daftar
	IsActive   bool    `json:"is_active"`         // dokumen aktif?
	ChunkCount int     `json:"chunk_count"`       // jumlah potongan (chunk)
	CreatedAt  string  `json:"created_at"`        // waktu dibuat
	UpdatedAt  string  `json:"updated_at"`        // waktu diubah
}

// DocumentInput: data tulis dokumen KB.
type DocumentInput struct {
	Title    string // judul
	Category string // kategori KB
	Content  string // isi dokumen
	IsActive bool   // status aktif
}

// Chunk: satu potongan dokumen aktif beserta metadata dokumennya (untuk indexing & pencarian).
type Chunk struct {
	ID         string // ID chunk: {document_id}#{index}
	DocumentID string // dokumen induk
	Title      string // judul dokumen
	Category   string // kategori dokumen
	Content    string // isi potongan
}

// ChunkResult: hasil pencarian satu chunk.
type ChunkResult struct {
	ChunkID    string   `json:"chunk_id"`    // ID chunk
	DocumentID string   `json:"document_id"` // dokumen induk
	Title      string   `json:"title"`       // judul dokumen
	Category   string   `json:"category"`    // kategori dokumen
	Content    string   `json:"content"`     // isi potongan
	Score      *float64 `json:"score"`       // null pada keyword_fallback
}

// Ptr mengembalikan pointer ke salinan nilai v (pembantu untuk field opsional).
func Ptr[T any](v T) *T { return &v }
