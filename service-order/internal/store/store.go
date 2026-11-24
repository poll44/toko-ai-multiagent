// Package store: akses order.db — skema sesuai TRD 03 §3.
// TODO(impl): seed 8 order, repo order, state machine.
package store // nama package: store

import ( // package yang dipakai
	"context"      // context
	"database/sql" // API database
	"fmt"          // format error

	"toko-ai-microservices/internal/shared/sqlitex" // pembuka SQLite
)

// schema: DDL tabel order.db (idempoten lewat IF NOT EXISTS).
const schema = `
-- tabel pesanan
CREATE TABLE IF NOT EXISTS orders (
  id                 TEXT PRIMARY KEY,   -- ID pesanan (ORD-xxx)
  user_id            TEXT NOT NULL,      -- pemilik pesanan
  status             TEXT NOT NULL CHECK (status IN ('PENDING_PAYMENT','PAID','PROCESSING','SHIPPED','DELIVERED','CANCELLED','PAYMENT_FAILED','PAYMENT_TIMEOUT')), -- status pesanan
  payment_status     TEXT NOT NULL CHECK (payment_status IN ('UNPAID','PAID','FAILED','EXPIRED','REFUNDED')), -- status pembayaran
  payment_method     TEXT NOT NULL CHECK (payment_method IN ('EWALLET','BANK_TRANSFER','CREDIT_CARD')),      -- metode pembayaran
  total_amount       INTEGER NOT NULL,   -- total harga
  shipping_address   TEXT NOT NULL,      -- alamat pengiriman
  tracking_number    TEXT,               -- nomor resi
  failure_reason     TEXT,               -- alasan gagal bayar
  payment_expires_at TEXT,               -- batas waktu bayar
  paid_at            TEXT,               -- waktu dibayar
  created_at         TEXT NOT NULL,      -- waktu dibuat
  updated_at         TEXT NOT NULL       -- waktu diubah
);
CREATE INDEX IF NOT EXISTS idx_orders_user ON orders(user_id, created_at DESC); -- percepat daftar pesanan per user
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);                 -- percepat filter status

-- tabel barang per pesanan
CREATE TABLE IF NOT EXISTS order_items (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,                  -- ID urut
  order_id     TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE, -- pesanan induk
  product_id   TEXT NOT NULL,                                      -- ID produk
  product_name TEXT NOT NULL,                                      -- nama produk (snapshot)
  unit_price   INTEGER NOT NULL,                                   -- harga satuan (snapshot)
  quantity     INTEGER NOT NULL CHECK (quantity > 0),              -- jumlah harus positif
  subtotal     INTEGER NOT NULL                                    -- harga x jumlah
);

-- tabel riwayat perubahan status pesanan
CREATE TABLE IF NOT EXISTS order_status_history (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,                  -- ID urut
  order_id    TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE, -- pesanan induk
  from_status TEXT,                                               -- status sebelumnya
  to_status   TEXT NOT NULL,                                      -- status baru
  note        TEXT,                                               -- catatan
  actor_id    TEXT NOT NULL,                                      -- pelaku
  created_at  TEXT NOT NULL                                       -- waktu
);`

// Open membuka order.db lalu menjalankan migrasi skema.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sqlitex.Open(path) // buka SQLite dengan pragma standar
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil { // jalankan DDL
		_ = db.Close() // tutup bila migrasi gagal
		return nil, fmt.Errorf("migrate order.db: %w", err)
	}
	return db, nil
}
