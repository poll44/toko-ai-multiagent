// Package sqlitex: pembuka SQLite pure-Go (modernc.org/sqlite, tanpa CGO) + helper transaksi.
package sqlitex // nama package: sqlitex

import ( // package yang dipakai
	"context"       // context transaksi
	"database/sql"  // API database standar
	"fmt"           // format error
	"os"            // membuat folder
	"path/filepath" // manipulasi path file
	"time"          // waktu

	_ "modernc.org/sqlite" // driver "sqlite"
)

// Open membuka (dan membuat folder induk) database SQLite dengan pragma standar:
// WAL, foreign_keys, busy_timeout 5 s, transaksi IMMEDIATE. Penulis tunggal (MaxOpenConns=1).
func Open(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" { // ada folder induk
		if err := os.MkdirAll(dir, 0o755); err != nil { // buat folder bila belum ada
			return nil, fmt.Errorf("buat folder database: %w", err)
		}
	}
	dsn := "file:" + filepath.ToSlash(path) + // DSN dengan path bergaya slash
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate" // pragma standar
	db, err := sql.Open("sqlite", dsn) // buka koneksi (lazy)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)             // satu koneksi: penulis tunggal
	if err := db.Ping(); err != nil { // verifikasi koneksi benar-benar jalan
		_ = db.Close() // tutup bila gagal
		return nil, fmt.Errorf("ping sqlite %q: %w", path, err)
	}
	return db, nil
}

// WithTx menjalankan fn dalam transaksi: commit bila nil, rollback bila error/panic.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil) // mulai transaksi
	if err != nil {
		return err
	}
	defer func() { // pembersihan saat selesai
		if p := recover(); p != nil { // terjadi panic
			_ = tx.Rollback() // batalkan transaksi
			panic(p)          // lempar ulang panic
		}
		if err != nil { // fungsi berakhir dengan error
			_ = tx.Rollback() // batalkan transaksi
		}
	}()
	if err = fn(tx); err != nil { // jalankan logika pemanggil
		return err
	}
	return tx.Commit() // simpan perubahan
}

// NowUTC: RFC 3339 UTC milidetik, mis. 2026-11-19T08:30:00.000Z.
func NowUTC() string { return FormatTime(time.Now()) }

// FormatTime memformat waktu apa pun ke RFC 3339 UTC milidetik.
func FormatTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
