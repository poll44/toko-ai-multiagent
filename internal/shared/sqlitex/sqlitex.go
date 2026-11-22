// Package sqlitex: pembuka SQLite pure-Go (modernc.org/sqlite, tanpa CGO) + helper transaksi.
package sqlitex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // driver "sqlite"
)

// Open membuka (dan membuat folder induk) database SQLite dengan pragma standar:
// WAL, foreign_keys, busy_timeout 5 s, transaksi IMMEDIATE. Penulis tunggal (MaxOpenConns=1).
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("buat folder database: %w", err)
		}
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", path, err)
	}
	return db, nil
}

// WithTx menjalankan fn dalam transaksi: commit bila nil, rollback bila error/panic.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// NowUTC: RFC 3339 UTC milidetik, mis. 2026-11-19T08:30:00.000Z.
func NowUTC() string { return FormatTime(time.Now()) }

// FormatTime memformat waktu apa pun ke RFC 3339 UTC milidetik.
func FormatTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
