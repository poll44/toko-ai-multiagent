// Package store: akses ticket.db — skema sesuai TRD 04 §3 (tiket + knowledge base + vektor).
// TODO(impl): seed 5 tiket & 8 dokumen KB, repo, chunker, indexer.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"toko-ai-microservices/internal/shared/sqlitex"
)

const schema = `
CREATE TABLE IF NOT EXISTS tickets (
  id              TEXT PRIMARY KEY,
  user_id         TEXT NOT NULL,
  order_id        TEXT,
  subject         TEXT NOT NULL,
  description     TEXT NOT NULL,
  category        TEXT NOT NULL CHECK (category IN ('PAYMENT','SHIPPING','PRODUCT','TECHNICAL','OTHER')),
  priority        TEXT NOT NULL DEFAULT 'MEDIUM' CHECK (priority IN ('LOW','MEDIUM','HIGH')),
  status          TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','IN_PROGRESS','RESOLVED','CLOSED')),
  assigned_to     TEXT,
  resolution_note TEXT,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  resolved_at     TEXT,
  closed_at       TEXT
);
CREATE INDEX IF NOT EXISTS idx_tickets_user ON tickets(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status);
CREATE INDEX IF NOT EXISTS idx_tickets_category ON tickets(category);

CREATE TABLE IF NOT EXISTS ticket_messages (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  ticket_id   TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
  author_id   TEXT NOT NULL,
  author_role TEXT NOT NULL,
  message     TEXT NOT NULL,
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ticket_status_history (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  ticket_id   TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
  from_status TEXT,
  to_status   TEXT NOT NULL,
  actor_id    TEXT NOT NULL,
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS knowledge_documents (
  id         TEXT PRIMARY KEY,
  title      TEXT NOT NULL,
  category   TEXT NOT NULL CHECK (category IN ('RETURN','WARRANTY','SHIPPING','PAYMENT','ORDER','COMPLAINT','GENERAL')),
  content    TEXT NOT NULL,
  is_active  INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS knowledge_chunks (
  id           TEXT PRIMARY KEY,      -- {document_id}#{chunk_index}
  document_id  TEXT NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
  chunk_index  INTEGER NOT NULL,
  content      TEXT NOT NULL,
  content_hash TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS knowledge_embeddings (
  chunk_id     TEXT PRIMARY KEY REFERENCES knowledge_chunks(id) ON DELETE CASCADE,
  model        TEXT NOT NULL,
  dim          INTEGER NOT NULL,
  content_hash TEXT NOT NULL,
  embedding    BLOB NOT NULL,         -- dim x float32 LE, ter-normalisasi L2
  updated_at   TEXT NOT NULL
);`

func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sqlitex.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate ticket.db: %w", err)
	}
	return db, nil
}
