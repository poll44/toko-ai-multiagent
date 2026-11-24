// Package store: akses ticket.db — skema sesuai TRD 04 §3 (tiket + knowledge base + vektor).
// TODO(impl): seed 5 tiket & 8 dokumen KB, repo, chunker, indexer.
package store // nama package: store

import ( // package yang dipakai
	"context"      // context
	"database/sql" // API database
	"fmt"          // format error

	"toko-ai-microservices/internal/shared/sqlitex" // pembuka SQLite
)

// schema: DDL tabel ticket.db (idempoten lewat IF NOT EXISTS).
const schema = `
-- tabel tiket komplain
CREATE TABLE IF NOT EXISTS tickets (
  id              TEXT PRIMARY KEY,    -- ID tiket (TKT-xxx)
  user_id         TEXT NOT NULL,       -- pemilik tiket
  order_id        TEXT,                -- pesanan terkait (opsional)
  subject         TEXT NOT NULL,       -- subjek
  description     TEXT NOT NULL,       -- deskripsi masalah
  category        TEXT NOT NULL CHECK (category IN ('PAYMENT','SHIPPING','PRODUCT','TECHNICAL','OTHER')),     -- kategori
  priority        TEXT NOT NULL DEFAULT 'MEDIUM' CHECK (priority IN ('LOW','MEDIUM','HIGH')),                -- prioritas
  status          TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','IN_PROGRESS','RESOLVED','CLOSED')), -- status
  assigned_to     TEXT,                -- admin penanggung jawab
  resolution_note TEXT,                -- catatan penyelesaian
  created_at      TEXT NOT NULL,       -- waktu dibuat
  updated_at      TEXT NOT NULL,       -- waktu diubah
  resolved_at     TEXT,                -- waktu diselesaikan
  closed_at       TEXT                 -- waktu ditutup
);
CREATE INDEX IF NOT EXISTS idx_tickets_user ON tickets(user_id, created_at DESC); -- percepat daftar tiket per user
CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status);                 -- percepat filter status
CREATE INDEX IF NOT EXISTS idx_tickets_category ON tickets(category);             -- percepat filter kategori

-- tabel pesan dalam tiket
CREATE TABLE IF NOT EXISTS ticket_messages (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,                    -- ID urut
  ticket_id   TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE, -- tiket induk
  author_id   TEXT NOT NULL,                                        -- penulis
  author_role TEXT NOT NULL,                                        -- role penulis
  message     TEXT NOT NULL,                                        -- isi pesan
  created_at  TEXT NOT NULL                                         -- waktu
);

-- tabel riwayat status tiket
CREATE TABLE IF NOT EXISTS ticket_status_history (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,                    -- ID urut
  ticket_id   TEXT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE, -- tiket induk
  from_status TEXT,                                                 -- status sebelumnya
  to_status   TEXT NOT NULL,                                        -- status baru
  actor_id    TEXT NOT NULL,                                        -- pelaku
  created_at  TEXT NOT NULL                                         -- waktu
);

-- tabel dokumen knowledge base
CREATE TABLE IF NOT EXISTS knowledge_documents (
  id         TEXT PRIMARY KEY,         -- ID dokumen (KB-xxx)
  title      TEXT NOT NULL,            -- judul
  category   TEXT NOT NULL CHECK (category IN ('RETURN','WARRANTY','SHIPPING','PAYMENT','ORDER','COMPLAINT','GENERAL')), -- kategori
  content    TEXT NOT NULL,            -- isi lengkap
  is_active  INTEGER NOT NULL DEFAULT 1, -- 1 = aktif
  created_at TEXT NOT NULL,            -- waktu dibuat
  updated_at TEXT NOT NULL             -- waktu diubah
);

-- tabel potongan (chunk) dokumen
CREATE TABLE IF NOT EXISTS knowledge_chunks (
  id           TEXT PRIMARY KEY,      -- {document_id}#{chunk_index}
  document_id  TEXT NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE, -- dokumen induk
  chunk_index  INTEGER NOT NULL,      -- urutan chunk
  content      TEXT NOT NULL,         -- isi chunk
  content_hash TEXT NOT NULL          -- hash isi (deteksi perubahan)
);

-- tabel vektor embedding per chunk
CREATE TABLE IF NOT EXISTS knowledge_embeddings (
  chunk_id     TEXT PRIMARY KEY REFERENCES knowledge_chunks(id) ON DELETE CASCADE, -- chunk terkait
  model        TEXT NOT NULL,         -- model embedding
  dim          INTEGER NOT NULL,      -- dimensi vektor
  content_hash TEXT NOT NULL,         -- hash isi saat di-embed
  embedding    BLOB NOT NULL,         -- dim x float32 LE, ter-normalisasi L2
  updated_at   TEXT NOT NULL          -- waktu diperbarui
);`

// Open membuka ticket.db lalu menjalankan migrasi skema.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sqlitex.Open(path) // buka SQLite dengan pragma standar
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil { // jalankan DDL
		_ = db.Close() // tutup bila migrasi gagal
		return nil, fmt.Errorf("migrate ticket.db: %w", err)
	}
	return db, nil
}
