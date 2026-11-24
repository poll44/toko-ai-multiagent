// Package store: akses auth.db (users, login_audit) — skema sesuai TRD 01 §3.
package store // nama package: store

import ( // package yang dipakai
	"context"      // context
	"database/sql" // API database
	"fmt"          // format error

	"toko-ai-microservices/internal/shared/sqlitex" // pembuka SQLite
)

// schema: DDL tabel auth.db (idempoten lewat IF NOT EXISTS).
const schema = `
-- tabel pengguna
CREATE TABLE IF NOT EXISTS users (
  id         TEXT PRIMARY KEY,           -- ID user (USR-xx / ADM-xx / DEV-xx)
  email      TEXT NOT NULL UNIQUE,       -- email unik
  password   TEXT NOT NULL,          -- plaintext "123456": KHUSUS DEMO
  name       TEXT NOT NULL,              -- nama lengkap
  role       TEXT NOT NULL CHECK (role IN ('CUSTOMER','ADMIN_SISTEM','ADMIN_DEV')), -- role yang valid
  is_active  INTEGER NOT NULL DEFAULT 1, -- 1 = aktif
  created_at TEXT NOT NULL               -- waktu dibuat
);
-- tabel audit percobaan login
CREATE TABLE IF NOT EXISTS login_audit (
  id         INTEGER PRIMARY KEY AUTOINCREMENT, -- ID urut
  user_id    TEXT,                              -- NULL bila email tidak dikenal
  email      TEXT NOT NULL,                     -- email yang dicoba
  success    INTEGER NOT NULL,                  -- 1 = berhasil, 0 = gagal
  ip         TEXT,                              -- alamat IP klien
  created_at TEXT NOT NULL                      -- waktu percobaan
);`

// demoUsers: 4 akun demo yang di-seed otomatis.
var demoUsers = []struct{ ID, Email, Name, Role string }{
	{"USR-01", "budi@toko.com", "Budi Santoso", "CUSTOMER"},      // pelanggan 1
	{"USR-02", "siti@toko.com", "Siti Rahma", "CUSTOMER"},        // pelanggan 2
	{"ADM-01", "admin@toko.com", "Admin Sistem", "ADMIN_SISTEM"}, // admin sistem
	{"DEV-01", "dev@toko.com", "Developer Toko", "ADMIN_DEV"},    // admin developer
}

// Open membuka auth.db, membuat skema, dan seed 4 akun demo (idempoten).
func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sqlitex.Open(path) // buka SQLite dengan pragma standar
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil { // jalankan DDL
		_ = db.Close()
		return nil, fmt.Errorf("migrate auth.db: %w", err)
	}
	now := sqlitex.NowUTC()       // waktu seed
	for _, u := range demoUsers { // sisipkan tiap akun demo
		if _, err := db.ExecContext(ctx,
			// INSERT OR IGNORE: lewati bila akun sudah ada (idempoten)
			`INSERT OR IGNORE INTO users (id,email,password,name,role,is_active,created_at) VALUES (?,?,?,?,?,1,?)`,
			u.ID, u.Email, "123456", u.Name, u.Role, now); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("seed users: %w", err)
		}
	}
	return db, nil
}
