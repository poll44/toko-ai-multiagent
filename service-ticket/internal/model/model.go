// Package model: tipe data service-ticket (JSON snake_case, PRD 04 §6–§7).
package model

const (
	StOpen       = "OPEN"
	StInProgress = "IN_PROGRESS"
	StResolved   = "RESOLVED"
	StClosed     = "CLOSED"
)

var (
	Categories   = []string{"PAYMENT", "SHIPPING", "PRODUCT", "TECHNICAL", "OTHER"}
	Priorities   = []string{"LOW", "MEDIUM", "HIGH"}
	Statuses     = []string{StOpen, StInProgress, StResolved, StClosed}
	KBCategories = []string{"RETURN", "WARRANTY", "SHIPPING", "PAYMENT", "ORDER", "COMPLAINT", "GENERAL"}
)

type Message struct {
	ID         int64  `json:"id"`
	AuthorID   string `json:"author_id"`
	AuthorRole string `json:"author_role"`
	Message    string `json:"message"`
	CreatedAt  string `json:"created_at"`
}

type History struct {
	FromStatus *string `json:"from_status"`
	ToStatus   string  `json:"to_status"`
	ActorID    string  `json:"actor_id"`
	CreatedAt  string  `json:"created_at"`
}

type Ticket struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	OrderID        *string   `json:"order_id"`
	Subject        string    `json:"subject"`
	Description    string    `json:"description"`
	Category       string    `json:"category"`
	Priority       string    `json:"priority"`
	Status         string    `json:"status"`
	AssignedTo     *string   `json:"assigned_to"`
	ResolutionNote *string   `json:"resolution_note"`
	CreatedAt      string    `json:"created_at"`
	UpdatedAt      string    `json:"updated_at"`
	ResolvedAt     *string   `json:"resolved_at"`
	ClosedAt       *string   `json:"closed_at"`
	Messages       []Message `json:"messages,omitempty"` // hanya detail
	History        []History `json:"history,omitempty"`  // hanya detail
}

type TicketFilter struct {
	UserID     *string
	Status     string
	Category   string
	Priority   string
	OrderID    string
	AssignedTo string
	Page       int
	Limit      int
}

// ---- Knowledge base ----

type Document struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Category   string  `json:"category"`
	Content    *string `json:"content,omitempty"` // tidak dikirim pada daftar
	IsActive   bool    `json:"is_active"`
	ChunkCount int     `json:"chunk_count"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

type DocumentInput struct {
	Title    string
	Category string
	Content  string
	IsActive bool
}

// Chunk: satu potongan dokumen aktif beserta metadata dokumennya (untuk indexing & pencarian).
type Chunk struct {
	ID         string
	DocumentID string
	Title      string
	Category   string
	Content    string
}

type ChunkResult struct {
	ChunkID    string   `json:"chunk_id"`
	DocumentID string   `json:"document_id"`
	Title      string   `json:"title"`
	Category   string   `json:"category"`
	Content    string   `json:"content"`
	Score      *float64 `json:"score"` // null pada keyword_fallback
}

func Ptr[T any](v T) *T { return &v }
