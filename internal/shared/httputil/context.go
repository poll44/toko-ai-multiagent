package httputil // helper HTTP bersama untuk semua service

import "context" // untuk menyimpan identitas di context request

// Role sesuai Master TRD §5.
const (
	RoleGuest       = "GUEST"        // pengunjung belum login
	RoleCustomer    = "CUSTOMER"     // pelanggan terdaftar
	RoleAdminSistem = "ADMIN_SISTEM" // admin operasional toko
	RoleAdminDev    = "ADMIN_DEV"    // admin pengembang
)

// Nama header (HTTP case-insensitive; Go mengkanonikalkan saat dikirim).
const (
	HeaderUserID      = "x-user-id"          // ID user yang diteruskan gateway
	HeaderUserRole    = "x-user-role"        // role user yang diteruskan gateway
	HeaderInternalKey = "x-internal-api-key" // kunci rahasia antar-service
	HeaderRequestID   = "x-request-id"       // ID untuk melacak satu request
	HeaderServiceName = "x-service-name"     // nama service pemanggil
)

// ValidRole: true bila role dikenal.
func ValidRole(r string) bool {
	switch r { // cocokkan dengan role yang dikenal
	case RoleGuest, RoleCustomer, RoleAdminSistem, RoleAdminDev:
		return true // role valid
	}
	return false // role tidak dikenal
}

// Identity adalah identitas pemanggil hasil IdentityMiddleware.
type Identity struct {
	UserID      string // ID user
	Role        string // role user
	RequestID   string // ID request
	ServiceName string // x-service-name (hanya service-ke-service)
}

// IsAdmin: ADMIN_SISTEM atau ADMIN_DEV.
func (i Identity) IsAdmin() bool { return i.Role == RoleAdminSistem || i.Role == RoleAdminDev }

type ctxKey int // tipe kunci privat agar tidak bentrok di context

const (
	identityKey  ctxKey = iota // kunci untuk Identity
	requestIDKey               // kunci untuk request ID
	holderKey                  // *Identity mutable untuk logging
)

// WithIdentity mengembalikan context baru yang membawa Identity.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey, id) // simpan identitas di context
}

// IdentityFrom mengembalikan Identity dari context (zero value bila belum ada).
func IdentityFrom(ctx context.Context) Identity {
	id, _ := ctx.Value(identityKey).(Identity) // ambil identitas (zero value bila tak ada)
	if id.RequestID == "" {                    // request ID belum terisi
		id.RequestID = RequestIDFrom(ctx) // lengkapi dari context
	}
	return id // kembalikan identitas
}

// RecordIdentity menyimpan identitas ke holder milik Logging (bila ada) agar log request mencatat user_id.
// Dipanggil oleh middleware yang menetapkan identitas (IdentityMiddleware di service domain, authenticate di gateway).
func RecordIdentity(ctx context.Context, id Identity) {
	if hp, ok := ctx.Value(holderKey).(*Identity); ok { // ada holder dari middleware Logging
		*hp = id // isi holder dengan identitas
	}
}

// WithRequestID mengembalikan context baru yang membawa request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id) // simpan request ID di context
}

// RequestIDFrom mengambil request ID dari context ("" bila tidak ada).
func RequestIDFrom(ctx context.Context) string {
	s, _ := ctx.Value(requestIDKey).(string) // ambil nilai bertipe string
	return s                                 // kembalikan request ID
}
