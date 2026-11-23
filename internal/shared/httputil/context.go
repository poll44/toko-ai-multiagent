package httputil

import "context"

// Role sesuai Master TRD §5.
const (
	RoleGuest       = "GUEST"
	RoleCustomer    = "CUSTOMER"
	RoleAdminSistem = "ADMIN_SISTEM"
	RoleAdminDev    = "ADMIN_DEV"
)

// Nama header (HTTP case-insensitive; Go mengkanonikalkan saat dikirim).
const (
	HeaderUserID      = "x-user-id"
	HeaderUserRole    = "x-user-role"
	HeaderInternalKey = "x-internal-api-key"
	HeaderRequestID   = "x-request-id"
	HeaderServiceName = "x-service-name"
)

// ValidRole: true bila role dikenal.
func ValidRole(r string) bool {
	switch r {
	case RoleGuest, RoleCustomer, RoleAdminSistem, RoleAdminDev:
		return true
	}
	return false
}

// Identity adalah identitas pemanggil hasil IdentityMiddleware.
type Identity struct {
	UserID      string
	Role        string
	RequestID   string
	ServiceName string // x-service-name (hanya service-ke-service)
}

// IsAdmin: ADMIN_SISTEM atau ADMIN_DEV.
func (i Identity) IsAdmin() bool { return i.Role == RoleAdminSistem || i.Role == RoleAdminDev }

type ctxKey int

const (
	identityKey ctxKey = iota
	requestIDKey
	holderKey // *Identity mutable untuk logging
)

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}

// IdentityFrom mengembalikan Identity dari context (zero value bila belum ada).
func IdentityFrom(ctx context.Context) Identity {
	id, _ := ctx.Value(identityKey).(Identity)
	if id.RequestID == "" {
		id.RequestID = RequestIDFrom(ctx)
	}
	return id
}

// RecordIdentity menyimpan identitas ke holder milik Logging (bila ada) agar log request mencatat user_id.
// Dipanggil oleh middleware yang menetapkan identitas (IdentityMiddleware di service domain, authenticate di gateway).
func RecordIdentity(ctx context.Context, id Identity) {
	if hp, ok := ctx.Value(holderKey).(*Identity); ok {
		*hp = id
	}
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestIDFrom(ctx context.Context) string {
	s, _ := ctx.Value(requestIDKey).(string)
	return s
}
