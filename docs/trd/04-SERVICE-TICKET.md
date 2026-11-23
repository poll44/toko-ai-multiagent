# 04 — TRD `service-ticket`

| Item | Nilai |
|---|---|
| Port | `4003` |
| Stack | **Go ≥ 1.22 native `net/http`**, `database/sql` + `modernc.org/sqlite` (pure-Go) |
| Database | `ticket.db` (`TICKET_DB_PATH`) |
| Peran | (1) Tiket komplain/bantuan + lifecycle; (2) **Knowledge Base kebijakan toko (FAQ/SOP) dengan pencarian vektor untuk RAG** |
| Dependensi | `service-order` (validasi kepemilikan order); Gemini Embedding API |
| Rujukan | `00-MASTER-TRD.md` (konvensi, §9 Vector & RAG) |

> Knowledge base ditempatkan di `service-ticket` (bukan service baru) karena kebijakan toko dipakai langsung untuk memutuskan apakah suatu keluhan perlu menjadi tiket.

## 1. Struktur Kode
```
service-ticket/
├── main.go                         # config, DB, seed, embedding worker, mux, graceful shutdown
└── internal/
    ├── config/config.go
    ├── store/{db.go,migrate.go,seed_tickets.go,seed_knowledge.go,ticket_repo.go,knowledge_repo.go}
    ├── orderclient/client.go       # GetOrder() — propagasi identitas asli user
    ├── service/ticket_service.go   # state machine tiket
    ├── knowledge/{chunker.go,indexer.go,search.go}
    └── handlers/{tickets.go,knowledge.go,health.go}
```
Memakai `internal/shared/*` (`httputil`, `sqlitex`, `validate`, `svcclient`, `embedding`, `vector`).

## 2. Routing & Middleware
```go
secured := httputil.Chain(httputil.InternalAuthMiddleware(key), httputil.IdentityMiddleware())  // GUEST lolos identity
authed  := func(h http.Handler) http.Handler { return secured(denyGuest(h)) }                    // GUEST → 403

// Tiket (tanpa GUEST)
mux.Handle("POST /api/tickets",                  authed(requireRole("CUSTOMER")(createTicket)))
mux.Handle("GET /api/tickets",                   authed(listTickets))
mux.Handle("GET /api/tickets/{id}",              authed(getTicket))
mux.Handle("POST /api/tickets/{id}/messages",    authed(addMessage))
mux.Handle("PATCH /api/tickets/{id}/status",     authed(updateStatus))

// Knowledge base (GUEST boleh baca/cari)
mux.Handle("GET /api/knowledge/search",          secured(searchKnowledge))
mux.Handle("GET /api/knowledge/documents",       secured(listDocs))
mux.Handle("GET /api/knowledge/documents/{id}",  secured(getDoc))
mux.Handle("POST /api/knowledge/documents",      secured(requireRole("ADMIN_SISTEM")(createDoc)))
mux.Handle("PUT /api/knowledge/documents/{id}",  secured(requireRole("ADMIN_SISTEM")(updateDoc)))
mux.Handle("DELETE /api/knowledge/documents/{id}", secured(requireRole("ADMIN_SISTEM")(deleteDoc)))
mux.Handle("POST /api/knowledge/reindex",        secured(requireRole("ADMIN_SISTEM")(reindex)))
mux.HandleFunc("GET /health", health)
```
Urutan global: `Recover → RequestID → Logging → [/health] → InternalAuth → Identity → (denyGuest) → RequireRole`.

## 3. Skema SQLite `ticket.db`

### 3.1 Tiket
#### Tabel `tickets`
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `id` | TEXT | PRIMARY KEY | `TKT-001`, … |
| `user_id` | TEXT | NOT NULL | pemilik, dari `x-user-id` |
| `order_id` | TEXT | NULL | relasi logis ke `order.db` |
| `subject` | TEXT | NOT NULL | 5–120 |
| `description` | TEXT | NOT NULL | 10–2000 |
| `category` | TEXT | NOT NULL CHECK(category IN ('PAYMENT','SHIPPING','PRODUCT','TECHNICAL','OTHER')) | |
| `priority` | TEXT | NOT NULL CHECK(priority IN ('LOW','MEDIUM','HIGH')) DEFAULT 'MEDIUM' | |
| `status` | TEXT | NOT NULL CHECK(status IN ('OPEN','IN_PROGRESS','RESOLVED','CLOSED')) DEFAULT 'OPEN' | |
| `assigned_to` | TEXT | NULL | `ADM-01` / `DEV-01` |
| `resolution_note` | TEXT | NULL | |
| `created_at` | TEXT | NOT NULL | RFC 3339 UTC |
| `updated_at` | TEXT | NOT NULL | |
| `resolved_at` | TEXT | NULL | |
| `closed_at` | TEXT | NULL | |

Index: `idx_tickets_user(user_id, created_at DESC)`, `idx_tickets_status(status)`, `idx_tickets_category(category)`.

#### Tabel `ticket_messages`
| Kolom | Tipe | Constraint |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `ticket_id` | TEXT | NOT NULL REFERENCES tickets(id) ON DELETE CASCADE |
| `author_id` | TEXT | NOT NULL |
| `author_role` | TEXT | NOT NULL |
| `message` | TEXT | NOT NULL (1–2000) |
| `created_at` | TEXT | NOT NULL |

#### Tabel `ticket_status_history`
| Kolom | Tipe | Constraint |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `ticket_id` | TEXT | NOT NULL REFERENCES tickets(id) ON DELETE CASCADE |
| `from_status` | TEXT | NULL |
| `to_status` | TEXT | NOT NULL |
| `actor_id` | TEXT | NOT NULL |
| `created_at` | TEXT | NOT NULL |

### 3.2 Knowledge Base (RAG)
#### Tabel `knowledge_documents`
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `id` | TEXT | PRIMARY KEY | `KB-001`, … |
| `title` | TEXT | NOT NULL | 5–120 |
| `category` | TEXT | NOT NULL CHECK(category IN ('RETURN','WARRANTY','SHIPPING','PAYMENT','ORDER','COMPLAINT','GENERAL')) | |
| `content` | TEXT | NOT NULL | teks lengkap (Markdown ringan), 20–6000 |
| `is_active` | INTEGER | NOT NULL DEFAULT 1 | |
| `created_at` | TEXT | NOT NULL | |
| `updated_at` | TEXT | NOT NULL | |

#### Tabel `knowledge_chunks`
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `id` | TEXT | PRIMARY KEY | `KB-001#1` (`{document_id}#{chunk_index}`) |
| `document_id` | TEXT | NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE | |
| `chunk_index` | INTEGER | NOT NULL | mulai 1 |
| `content` | TEXT | NOT NULL | potongan teks |
| `content_hash` | TEXT | NOT NULL | SHA-256 dari teks embedding |

#### Tabel `knowledge_embeddings` (indeks vektor)
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `chunk_id` | TEXT | PRIMARY KEY REFERENCES knowledge_chunks(id) ON DELETE CASCADE | |
| `model` | TEXT | NOT NULL | mis. `text-embedding-004` |
| `dim` | INTEGER | NOT NULL | 768 |
| `content_hash` | TEXT | NOT NULL | untuk skip re-embed |
| `embedding` | BLOB | NOT NULL | float32 LE ter-normalisasi L2 |
| `updated_at` | TEXT | NOT NULL | |

Penyimpanan & pencarian vektor: **sama dengan catalog** (BLOB + cosine brute-force di Go lewat `vector.Store`; `sqlite-vec`/Qdrant adalah pengganti opsional karena `modernc.org/sqlite` tidak dapat memuat ekstensi C — lihat `02-SERVICE-CATALOG.md` §3.1).

#### Chunking (`knowledge/chunker.go`)
- Pisah per paragraf (baris kosong) → gabungkan sampai ≤ **800 karakter** per chunk; tanpa overlap.
- Teks embedding chunk: `"{title}\n{content}"` (judul ikut agar konteks terjaga).
- Edit dokumen → hapus chunk lama, buat ulang, embed ulang hanya chunk yang `content_hash`-nya baru.

## 4. Siklus Hidup Tiket
```
          ┌──────────── reopen (CUSTOMER pemilik) ─────────────┐
          ▼                                                    │
 [OPEN] ──assign/start──► [IN_PROGRESS] ──resolve──► [RESOLVED] ┤
   │                            │                        │      │
   │                            └──(kembali) OPEN         └─close─► [CLOSED] (final)
   └──────────────close (CUSTOMER pemilik / ADMIN) ───────────────────▲
```
| Dari | Ke | Pelaku | Syarat |
|---|---|---|---|
| `OPEN` | `IN_PROGRESS` | ADMIN_SISTEM; ADMIN_DEV (hanya `TECHNICAL`) | `assigned_to = x-user-id` |
| `IN_PROGRESS` | `RESOLVED` | sama | `resolution_note` wajib (≥10), set `resolved_at` |
| `IN_PROGRESS` | `OPEN` | ADMIN_* | `assigned_to=null` |
| `RESOLVED` | `OPEN` | CUSTOMER pemilik, ADMIN_* | *reopen*; wajib `message`; `resolved_at=null` |
| `RESOLVED` | `CLOSED` | CUSTOMER pemilik, ADMIN_* | set `closed_at` |
| `OPEN` | `CLOSED` | CUSTOMER pemilik, ADMIN_SISTEM | tutup langsung |
| `CLOSED` | — | — | **final** |

Lainnya → `409 INVALID_STATE_TRANSITION`. Pesan ke tiket `CLOSED` → `409`. ADMIN_DEV pada kategori non-`TECHNICAL` → `403 FORBIDDEN`. Kategori `TECHNICAL` dari customer: `priority` minimal `MEDIUM`. Transisi diimplementasikan sebagai `map[Status][]Status` + pemeriksaan pelaku.

## 5. Data Seed

### 5.1 Tiket (bila `tickets` kosong)
| `id` | `user_id` | `order_id` | `category` | `priority` | `status` | `assigned_to` | Subjek |
|---|---|---|---|---|---|---|---|
| `TKT-001` | `USR-01` | `ORD-003` | `PAYMENT` | `HIGH` | `OPEN` | — | "Pembayaran ORD-003 gagal padahal saldo cukup" |
| `TKT-002` | `USR-01` | `ORD-001` | `PRODUCT` | `LOW` | `RESOLVED` | `ADM-01` | "Pertanyaan garansi earbuds" (+`resolution_note`) |
| `TKT-003` | `USR-02` | `ORD-005` | `SHIPPING` | `MEDIUM` | `IN_PROGRESS` | `ADM-01` | "Estimasi pengiriman pesanan belum jelas" |
| `TKT-004` | `USR-02` | — | `TECHNICAL` | `MEDIUM` | `OPEN` | — | "Halaman checkout error saat memilih e-wallet" |
| `TKT-005` | `USR-02` | `ORD-008` | `PRODUCT` | `LOW` | `CLOSED` | `ADM-01` | "Webcam terbungkus kurang rapi" |

Tiap tiket ≥1 `ticket_messages` (pesan awal = `description`) + history konsisten; `TKT-003` punya balasan admin.

### 5.2 Knowledge Base (bila `knowledge_documents` kosong)
Kebijakan **demo** (fiktif) — wajib konsisten dengan `03-SERVICE-ORDER.md` (batas bayar 15 menit; stok dikembalikan saat gagal/timeout).

| `id` | `category` | `title` | Isi ringkas (ditulis penuh di seed, Bahasa Indonesia) |
|---|---|---|---|
| `KB-001` | `RETURN` | Syarat dan Prosedur Retur | Retur maks **7 hari** sejak diterima; produk belum dipakai & kemasan utuh; produk cacat/salah kirim wajib foto/video unboxing; ongkir retur ditanggung toko bila cacat/salah kirim, ditanggung pembeli bila alasan lain; produk elektronik yang sudah diaktifkan tidak dapat diretur kecuali cacat |
| `KB-002` | `WARRANTY` | Kebijakan Garansi | Garansi toko **12 bulan** untuk kerusakan pabrik; tidak mencakup benda jatuh, terkena cairan, modifikasi; klaim lewat tiket kategori `PRODUCT` dengan nomor pesanan; proses 3–14 hari kerja; aksesoris (kabel, charger) garansi 6 bulan |
| `KB-003` | `SHIPPING` | Estimasi Pengiriman | Jabodetabek 1–2 hari kerja; Jawa 2–4 hari kerja; luar Jawa 4–7 hari kerja; resi diterbitkan saat status `SHIPPED`; pesanan diproses setelah pembayaran `PAID`; keterlambatan > estimasi +3 hari → buat tiket `SHIPPING` |
| `KB-004` | `COMPLAINT` | Prosedur Komplain | Langkah: cek status pesanan & kebijakan terkait → bila belum terjawab buat tiket (subjek, kategori, nomor pesanan); respons awal maks 1×24 jam kerja; status tiket `OPEN → IN_PROGRESS → RESOLVED → CLOSED`; tiket `RESOLVED` dapat dibuka kembali oleh pelanggan |
| `KB-005` | `PAYMENT` | Pembayaran Gagal dan Kedaluwarsa | Batas bayar **15 menit** setelah pesanan dibuat; gagal/kedaluwarsa → status `PAYMENT_FAILED`/`PAYMENT_TIMEOUT`, **stok dikembalikan**, pesanan final dan tidak bisa dibayar ulang → buat pesanan baru; dana tidak boleh terpotong untuk pembayaran gagal — jika saldo terpotong, buat tiket `PAYMENT` prioritas HIGH dengan bukti |
| `KB-006` | `ORDER` | Pembatalan Pesanan | Pelanggan dapat membatalkan sendiri hanya saat `PENDING_PAYMENT`; setelah `PAID` pembatalan hanya oleh admin via tiket; pesanan `SHIPPED` tidak dapat dibatalkan (gunakan retur setelah diterima) |
| `KB-007` | `PAYMENT` | Kebijakan Refund | Refund ke metode pembayaran asal, **3–7 hari kerja** setelah disetujui; pesanan yang dibatalkan admin setelah `PAID` otomatis `REFUNDED`; retur disetujui → refund setelah barang diterima & diperiksa |
| `KB-008` | `GENERAL` | Jam Layanan dan Kontak | Layanan pelanggan Senin–Jumat 09.00–17.00 WIB; di luar jam, tiket tetap diterima dan diproses hari kerja berikutnya; satu-satunya kanal resmi: tiket di aplikasi & asisten AI |

Catatan: versi demo tidak membebankan ongkos kirim terpisah (total pesanan = total harga barang), sehingga dokumen pengiriman tidak memuat biaya. Tiap dokumen berisi 2–3 paragraf (≥ 300 karakter) agar menghasilkan 1–3 chunk. Seed memicu `IndexAll` (batch embedding) di background.

## 6. Kontrak REST API — Tiket

Konvensi: Master §7. Representasi `Ticket`:
```json
{
  "id": "TKT-001", "user_id": "USR-01", "order_id": "ORD-003",
  "subject": "...", "description": "...", "category": "PAYMENT", "priority": "HIGH",
  "status": "OPEN", "assigned_to": null, "resolution_note": null,
  "created_at": "...", "updated_at": "...", "resolved_at": null, "closed_at": null
}
```
Detail menambah `messages: [{id,author_id,author_role,message,created_at}]` dan `history: [...]`.

| Method | Path | Role | Deskripsi |
|---|---|---|---|
| POST | `/api/tickets` | CUSTOMER | Buat tiket |
| GET | `/api/tickets` | CUSTOMER (milik), ADMIN_SISTEM/ADMIN_DEV (semua) | Daftar |
| GET | `/api/tickets/{id}` | sama | Detail + pesan |
| POST | `/api/tickets/{id}/messages` | CUSTOMER pemilik, ADMIN_SISTEM, ADMIN_DEV (hanya `TECHNICAL`) | Tambah pesan |
| PATCH | `/api/tickets/{id}/status` | lihat §4 | Ubah status |

### 6.1 `POST /api/tickets` (CUSTOMER)
```json
{ "subject": "Pembayaran gagal", "description": "Saldo sudah cukup tetapi tetap gagal.",
  "category": "PAYMENT", "priority": "HIGH", "order_id": "ORD-003" }
```
| Field | Validasi |
|---|---|
| `subject` | wajib 5–120 |
| `description` | wajib 10–2000 |
| `category` | wajib enum |
| `priority` | opsional, default `MEDIUM` |
| `order_id` | opsional, `^ORD-\d{3,}$` |

Bila `order_id` ada: `orderclient.GetOrder(ctx, id)` → `GET {ORDER_URL}/api/orders/{id}` dengan identitas **asli user** + `x-internal-api-key` + `x-service-name: service-ticket`. `404` → `404 NOT_FOUND` (`details.field="order_id"`); jaringan → `502 UPSTREAM_ERROR` (tiket tidak dibuat); timeout → `504`.
Maks 1 tiket aktif (`OPEN`/`IN_PROGRESS`) per `(user_id, order_id)` → `409 CONFLICT` dengan `details.existing_ticket_id`. `201 { "data": Ticket }`; `user_id` dari `x-user-id`; pesan pertama otomatis.

### 6.2 `GET /api/tickets`
Query: `status`, `category`, `priority`, `order_id`, `user_id` (hanya ADMIN_*), `assigned_to` (hanya ADMIN_*; `me`=`x-user-id`), `page`, `limit` (1/20, maks 100). Urut `created_at DESC`.

### 6.3 `GET /api/tickets/{id}`
CUSTOMER bukan pemilik → `404 NOT_FOUND`.

### 6.4 `POST /api/tickets/{id}/messages`
`{ "message": "Saya sudah isi saldo lagi." }` → `201 { "data": {message} }`. Update `updated_at`. Tiket `CLOSED` → `409`.

### 6.5 `PATCH /api/tickets/{id}/status`
`{ "status": "RESOLVED", "resolution_note": "...", "message": "opsional" }` → `200 { "data": Ticket }` + history.

## 7. Kontrak REST API — Knowledge Base

| Method | Path | Role | Deskripsi |
|---|---|---|---|
| GET | `/api/knowledge/search` | **semua termasuk GUEST** | **Pencarian semantik kebijakan (RAG retrieval)** |
| GET | `/api/knowledge/documents` | semua | Daftar dokumen aktif (admin: `include_inactive=true`) |
| GET | `/api/knowledge/documents/{id}` | semua | Dokumen lengkap |
| POST | `/api/knowledge/documents` | ADMIN_SISTEM | Buat dokumen (+chunk +embedding) |
| PUT | `/api/knowledge/documents/{id}` | ADMIN_SISTEM | Ubah (re-chunk, re-embed chunk berubah) |
| DELETE | `/api/knowledge/documents/{id}` | ADMIN_SISTEM | Soft delete (`is_active=0`) |
| POST | `/api/knowledge/reindex` | ADMIN_SISTEM | Re-embed semua chunk → `202 {"data":{"queued":N}}` |

### 7.1 `GET /api/knowledge/search`
| Query | Tipe | Default | Validasi |
|---|---|---|---|
| `q` | string | — | **wajib**, 3–300 karakter |
| `limit` | integer | 3 | 1–5 |
| `category` | enum kategori KB | — | opsional |
| `min_score` | float 0–1 | `SEMANTIC_MIN_SCORE` (0.55) | |

Algoritma: embed `q` (`RETRIEVAL_QUERY`) → cosine terhadap semua chunk dokumen **aktif** → filter kategori & `min_score` → top `limit` (maks 2 chunk per dokumen agar beragam).

Respons `200`:
```json
{
  "data": [
    { "chunk_id": "KB-005#1", "document_id": "KB-005", "title": "Pembayaran Gagal dan Kedaluwarsa",
      "category": "PAYMENT", "content": "Batas waktu pembayaran adalah 15 menit ...", "score": 0.8431 }
  ],
  "meta": { "total": 1, "limit": 3, "query": "kenapa pesanan saya hangus padahal belum bayar", "mode": "semantic", "min_score": 0.55 }
}
```
| `meta.mode` | Arti |
|---|---|
| `semantic` | hasil vektor |
| `keyword_fallback` | embedding tidak tersedia → `LIKE` pada `title`/`content`, `score: null` |

Error: `400 VALIDATION_ERROR` (q), `503 EMBEDDING_UNAVAILABLE` (hanya jika fallback pun tak mungkin). Tidak ada hasil → `data: []`, `200` — **konsumen (agent) wajib menafsirkan sebagai "kebijakan tidak ditemukan", bukan mengarang**. Isi `content` dikembalikan apa adanya (sumber kutipan).

### 7.2 Dokumen
`POST/PUT` body: `{ "title", "category", "content", "is_active"(PUT) }`. Respons `Document`: `{id,title,category,content,is_active,chunk_count,created_at,updated_at}`; daftar tanpa `content`. Embedding sinkron dengan timeout `EMBEDDING_TIMEOUT_MS`; gagal → tetap tersimpan, ditangani worker 60 detik (sama seperti catalog).

## 8. Isolasi Data & RBAC
- CUSTOMER: query tiket diberi `WHERE user_id = :x-user-id`; id lain → `404`.
- ADMIN_SISTEM: semua data & transisi. ADMIN_DEV: baca semua; tulis hanya kategori `TECHNICAL`.
- `user_id`/`assigned_to` tidak pernah dari body.
- Knowledge base: baca publik (GUEST ok); tulis ADMIN_SISTEM saja.

## 9. Bootstrap `main.go`
1. `config.Load()` (wajib `INTERNAL_API_KEY`, `ORDER_URL`; `GOOGLE_API_KEY` opsional → fallback keyword).
2. `sqlitex.Open` → migrate → seed tiket & KB.
3. `knowledge.Worker(ctx)` (ticker 60 s) + `IndexAll` awal non-blocking.
4. `http.Server{Addr: ":"+TICKET_PORT}` + graceful shutdown.

## 10. Kriteria Penerimaan
- [ ] `CGO_ENABLED=0 go build ./service-ticket` sukses.
- [ ] Budi `GET /api/tickets` → TKT-001, TKT-002 saja; `GET /api/tickets/TKT-003` → `404`.
- [ ] Budi membuat tiket untuk `ORD-005` → `404 NOT_FOUND`; duplikat aktif `ORD-003` → `409 CONFLICT`.
- [ ] `OPEN → RESOLVED` langsung → `409`; `CLOSED` tak dapat diubah.
- [ ] `DEV-01` mengubah status tiket `PAYMENT` → `403`; `TKT-004` → berhasil.
- [ ] GUEST `GET /api/tickets` → `403`; GUEST `GET /api/knowledge/search?q=...` → `200`.
- [ ] `GET /api/knowledge/search?q=berapa lama batas waktu bayar` → `KB-005` peringkat 1, `meta.mode="semantic"`.
- [ ] `q="syarat pengembalian barang rusak"` (tanpa kata "retur") → `KB-001` muncul (bukti semantik, bukan keyword).
- [ ] `q="resep rendang"` → `data: []`.
- [ ] Tanpa `GOOGLE_API_KEY`: service hidup; search → `keyword_fallback`.
- [ ] CUSTOMER `POST /api/knowledge/documents` → `403`; ADMIN_SISTEM menambah dokumen → dapat ditemukan lewat search.
- [ ] Tanpa `x-internal-api-key` → `401 INVALID_INTERNAL_KEY`.
