// Package store: akses catalog.db — skema sesuai PRD 02 §3.
// TODO(impl): seed 14 produk, repo produk/stok/embedding.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"toko-ai-microservices/internal/shared/sqlitex"
)

const schema = `
CREATE TABLE IF NOT EXISTS products (
  id             TEXT PRIMARY KEY,
  name           TEXT NOT NULL,
  description    TEXT NOT NULL DEFAULT '',
  specifications TEXT NOT NULL DEFAULT '',
  category       TEXT NOT NULL,
  price          INTEGER NOT NULL CHECK (price >= 0),
  stock          INTEGER NOT NULL CHECK (stock >= 0),
  image_url      TEXT,
  is_active      INTEGER NOT NULL DEFAULT 1,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_products_category ON products(category);
CREATE INDEX IF NOT EXISTS idx_products_name ON products(name);

CREATE TABLE IF NOT EXISTS product_embeddings (
  product_id   TEXT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
  model        TEXT NOT NULL,
  dim          INTEGER NOT NULL,
  content_hash TEXT NOT NULL,
  embedding    BLOB NOT NULL,        -- dim x float32 LE, ter-normalisasi L2
  updated_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS stock_movements (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  product_id   TEXT NOT NULL REFERENCES products(id),
  delta        INTEGER NOT NULL,
  stock_after  INTEGER NOT NULL,
  reason       TEXT NOT NULL CHECK (reason IN ('SEED','ORDER_RESERVE','ORDER_RESTOCK','ADMIN_ADJUST')),
  reference_id TEXT,
  actor_id     TEXT NOT NULL,
  created_at   TEXT NOT NULL
);`

func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sqlitex.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate catalog.db: %w", err)
	}
	return db, nil
}
