// Package store: akses catalog.db — skema sesuai TRD 02 §3.
// TODO(impl): seed 14 produk, repo produk/stok/embedding.
package store // nama package: store

import ( // package yang dipakai
	"context"      // context
	"database/sql" // API database
	"fmt"          // format error

	"toko-ai-microservices/internal/shared/sqlitex" // pembuka SQLite
)

// schema: DDL seluruh tabel catalog.db (idempoten lewat IF NOT EXISTS).
const schema = `
-- tabel produk
CREATE TABLE IF NOT EXISTS products (
  id             TEXT PRIMARY KEY,                       -- ID produk (PRD-xxx)
  name           TEXT NOT NULL,                          -- nama produk
  description    TEXT NOT NULL DEFAULT '',               -- deskripsi
  specifications TEXT NOT NULL DEFAULT '',               -- spesifikasi
  category       TEXT NOT NULL,                          -- kategori
  price          INTEGER NOT NULL CHECK (price >= 0),    -- harga tidak boleh negatif
  stock          INTEGER NOT NULL CHECK (stock >= 0),    -- stok tidak boleh negatif
  image_url      TEXT,                                   -- URL gambar (opsional)
  is_active      INTEGER NOT NULL DEFAULT 1,             -- 1 = aktif, 0 = nonaktif
  created_at     TEXT NOT NULL,                          -- waktu dibuat
  updated_at     TEXT NOT NULL                           -- waktu diubah
);
CREATE INDEX IF NOT EXISTS idx_products_category ON products(category); -- percepat filter kategori
CREATE INDEX IF NOT EXISTS idx_products_name ON products(name);         -- percepat pencarian nama

-- tabel vektor embedding per produk
CREATE TABLE IF NOT EXISTS product_embeddings (
  product_id   TEXT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE, -- produk terkait
  model        TEXT NOT NULL,                                              -- model embedding
  dim          INTEGER NOT NULL,                                           -- dimensi vektor
  content_hash TEXT NOT NULL,                                              -- hash isi (deteksi perubahan)
  embedding    BLOB NOT NULL,        -- dim x float32 LE, ter-normalisasi L2
  updated_at   TEXT NOT NULL                                               -- waktu diperbarui
);

-- tabel riwayat perubahan stok (audit)
CREATE TABLE IF NOT EXISTS stock_movements (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,                -- ID urut
  product_id   TEXT NOT NULL REFERENCES products(id),            -- produk terkait
  delta        INTEGER NOT NULL,                                 -- perubahan stok (+/-)
  stock_after  INTEGER NOT NULL,                                 -- stok sesudah perubahan
  reason       TEXT NOT NULL CHECK (reason IN ('SEED','ORDER_RESERVE','ORDER_RESTOCK','ADMIN_ADJUST')), -- alasan
  reference_id TEXT,                                             -- referensi (mis. ID pesanan)
  actor_id     TEXT NOT NULL,                                    -- pelaku
  created_at   TEXT NOT NULL                                     -- waktu
);`

// Open membuka catalog.db lalu menjalankan migrasi skema.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sqlitex.Open(path) // buka SQLite dengan pragma standar
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil { // jalankan DDL
		_ = db.Close() // tutup bila migrasi gagal
		return nil, fmt.Errorf("migrate catalog.db: %w", err)
	}
	return db, nil
}
