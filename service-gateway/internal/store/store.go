// Package store: akses auth.db (users, login_audit) — skema sesuai PRD 01 §3.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"toko-ai-microservices/internal/shared/sqlitex"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id         TEXT PRIMARY KEY,
  email      TEXT NOT NULL UNIQUE,
  password   TEXT NOT NULL,          -- plaintext "123456": KHUSUS DEMO
  name       TEXT NOT NULL,
  role       TEXT NOT NULL CHECK (role IN ('CUSTOMER','ADMIN_SISTEM','ADMIN_DEV')),
  is_active  INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS login_audit (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    TEXT,
  email      TEXT NOT NULL,
  success    INTEGER NOT NULL,
  ip         TEXT,
  created_at TEXT NOT NULL
);`

var demoUsers = []struct{ ID, Email, Name, Role string }{
	{"USR-01", "budi@toko.com", "Budi Santoso", "CUSTOMER"},
	{"USR-02", "siti@toko.com", "Siti Rahma", "CUSTOMER"},
	{"ADM-01", "admin@toko.com", "Admin Sistem", "ADMIN_SISTEM"},
	{"DEV-01", "dev@toko.com", "Developer Toko", "ADMIN_DEV"},
}

// Open membuka auth.db, membuat skema, dan seed 4 akun demo (idempoten).
func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sqlitex.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate auth.db: %w", err)
	}
	now := sqlitex.NowUTC()
	for _, u := range demoUsers {
		if _, err := db.ExecContext(ctx,
			`INSERT OR IGNORE INTO users (id,email,password,name,role,is_active,created_at) VALUES (?,?,?,?,?,1,?)`,
			u.ID, u.Email, "123456", u.Name, u.Role, now); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("seed users: %w", err)
		}
	}
	return db, nil
}
