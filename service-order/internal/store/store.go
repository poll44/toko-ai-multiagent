// Package store: akses order.db — skema sesuai PRD 03 §3.
// TODO(impl): seed 8 order, repo order, state machine.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"toko-ai-microservices/internal/shared/sqlitex"
)

const schema = `
CREATE TABLE IF NOT EXISTS orders (
  id                 TEXT PRIMARY KEY,
  user_id            TEXT NOT NULL,
  status             TEXT NOT NULL CHECK (status IN ('PENDING_PAYMENT','PAID','PROCESSING','SHIPPED','DELIVERED','CANCELLED','PAYMENT_FAILED','PAYMENT_TIMEOUT')),
  payment_status     TEXT NOT NULL CHECK (payment_status IN ('UNPAID','PAID','FAILED','EXPIRED','REFUNDED')),
  payment_method     TEXT NOT NULL CHECK (payment_method IN ('EWALLET','BANK_TRANSFER','CREDIT_CARD')),
  total_amount       INTEGER NOT NULL,
  shipping_address   TEXT NOT NULL,
  tracking_number    TEXT,
  failure_reason     TEXT,
  payment_expires_at TEXT,
  paid_at            TEXT,
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_orders_user ON orders(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);

CREATE TABLE IF NOT EXISTS order_items (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  order_id     TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  product_id   TEXT NOT NULL,
  product_name TEXT NOT NULL,
  unit_price   INTEGER NOT NULL,
  quantity     INTEGER NOT NULL CHECK (quantity > 0),
  subtotal     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS order_status_history (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  order_id    TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  from_status TEXT,
  to_status   TEXT NOT NULL,
  note        TEXT,
  actor_id    TEXT NOT NULL,
  created_at  TEXT NOT NULL
);`

func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sqlitex.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate order.db: %w", err)
	}
	return db, nil
}
