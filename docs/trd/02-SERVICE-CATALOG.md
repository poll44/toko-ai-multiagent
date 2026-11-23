# 02 — TRD `service-catalog`

| Item | Nilai |
|---|---|
| Port | `4001` |
| Stack | **Go ≥ 1.22 native `net/http`** (`http.ServeMux`), `database/sql` + `modernc.org/sqlite` (pure-Go, tanpa CGO) |
| Database | `catalog.db` (`CATALOG_DB_PATH`) |
| Peran | Sumber kebenaran produk, harga, stok **+ indeks vektor untuk pencarian semantik** |
| Dependensi eksternal | Gemini Embedding API (`EMBEDDING_MODEL`, default `text-embedding-004`) |
| Rujukan | `00-MASTER-TRD.md` (konvensi JSON/error/header, shared package, §9 Vector & RAG) |

## 1. Struktur Kode
```
service-catalog/
├── main.go                         # config, DB, seed, embedding worker, mux, graceful shutdown
└── internal/
    ├── config/config.go
    ├── store/{db.go,migrate.go,seed.go,product_repo.go,stock_repo.go,embedding_repo.go}
    ├── handlers/{products.go,stock.go,internal_stock.go,semantic.go,health.go}
    ├── embed/{text.go,indexer.go}   # susun teks embedding, indexer + worker retry
    └── model/product.go
```
Memakai `internal/shared/*`: `httputil` (`WriteJSON`, `WriteData`, `WriteList`, `WriteError`, `ReadJSON`, `RequestID`, `Recover`, `Chain`, `InternalAuthMiddleware`, `IdentityMiddleware`, `RequireRole`, `RequireServiceCaller`), `sqlitex`, `validate`, `embedding` (klien Gemini), `vector` (encode/decode float32, cosine, top-K).

## 2. Routing & Middleware

```go
public := httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)
mux.HandleFunc("GET /health", health)                          // tanpa internal key
secured := httputil.Chain(httputil.InternalAuthMiddleware(cfg.InternalAPIKey),
                          httputil.IdentityMiddleware())       // validasi x-user-id/x-user-role
mux.Handle("GET /api/products",                         secured(list))
mux.Handle("GET /api/products/categories",              secured(categories))
mux.Handle("GET /api/products/semantic-search",         secured(semantic))
mux.Handle("GET /api/products/{id}",                    secured(get))
mux.Handle("POST /api/products",                        secured(requireRole("ADMIN_SISTEM")(create)))
mux.Handle("PUT /api/products/{id}",                    secured(requireRole("ADMIN_SISTEM")(update)))
mux.Handle("DELETE /api/products/{id}",                 secured(requireRole("ADMIN_SISTEM")(del)))
mux.Handle("PATCH /api/products/{id}/stock",            secured(requireRole("ADMIN_SISTEM")(adjustStock)))
mux.Handle("GET /api/products/{id}/stock-movements",    secured(requireRole("ADMIN_SISTEM","ADMIN_DEV")(movements)))
mux.Handle("POST /api/products/reindex",                secured(requireRole("ADMIN_SISTEM")(reindex)))
mux.Handle("POST /internal/stock/decrement",            secured(requireServiceCaller("service-order")(decrement)))
mux.Handle("POST /internal/stock/restock",              secured(requireServiceCaller("service-order")(restock)))
```
`ServeMux` Go 1.22 memilih pola paling spesifik: literal `categories`, `semantic-search`, `reindex` otomatis menang atas `{id}` — tidak bergantung urutan pendaftaran.

| Middleware | Perilaku |
|---|---|
| `InternalAuthMiddleware(key)` | Bandingkan `x-internal-api-key` dengan `subtle.ConstantTimeCompare`; gagal → `401 INVALID_INTERNAL_KEY` |
| `IdentityMiddleware()` | Baca `x-user-id` & `x-user-role`; role ∈ {`GUEST`,`CUSTOMER`,`ADMIN_SISTEM`,`ADMIN_DEV`} else `403 FORBIDDEN`; simpan `Identity{UserID, Role, RequestID, ServiceName}` di `context` |
| `RequireRole(...)` | Role tak termasuk → `403 FORBIDDEN` |
| `RequireServiceCaller(name)` | `x-service-name` ≠ `name` → `403 FORBIDDEN` |

## 3. Skema SQLite `catalog.db`

### Tabel `products`
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `id` | TEXT | PRIMARY KEY | `PRD-001`, … (`PRD-` + 3 digit naik) |
| `name` | TEXT | NOT NULL | |
| `description` | TEXT | NOT NULL DEFAULT '' | |
| `specifications` | TEXT | NOT NULL DEFAULT '' | Teks spesifikasi bebas (mis. "Konektivitas: Bluetooth 5.3; Baterai: 40 jam") — ikut diembedding |
| `category` | TEXT | NOT NULL | `audio`, `aksesoris`, `wearable`, `komputer`, `penyimpanan` |
| `price` | INTEGER | NOT NULL CHECK(price >= 0) | Rupiah |
| `stock` | INTEGER | NOT NULL CHECK(stock >= 0) | |
| `image_url` | TEXT | NULL | |
| `is_active` | INTEGER | NOT NULL DEFAULT 1 | 0 = tersembunyi dari publik |
| `created_at` | TEXT | NOT NULL | RFC 3339 UTC |
| `updated_at` | TEXT | NOT NULL | |

Index: `idx_products_category(category)`, `idx_products_name(name)`.

### Tabel `product_embeddings` (indeks vektor)
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `product_id` | TEXT | PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE | 1 vektor per produk |
| `model` | TEXT | NOT NULL | mis. `text-embedding-004` |
| `dim` | INTEGER | NOT NULL | mis. 768 |
| `content_hash` | TEXT | NOT NULL | SHA-256 dari teks yang diembedding; jika sama → tidak re-embed |
| `embedding` | BLOB | NOT NULL | `dim` × float32 little-endian, **sudah dinormalisasi L2** |
| `updated_at` | TEXT | NOT NULL | |

### Tabel `stock_movements`
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `product_id` | TEXT | NOT NULL REFERENCES products(id) | |
| `delta` | INTEGER | NOT NULL | negatif = keluar |
| `stock_after` | INTEGER | NOT NULL | |
| `reason` | TEXT | NOT NULL CHECK(reason IN ('SEED','ORDER_RESERVE','ORDER_RESTOCK','ADMIN_ADJUST')) | |
| `reference_id` | TEXT | NULL | mis. `ORD-009` |
| `actor_id` | TEXT | NOT NULL | `x-user-id` pemanggil |
| `created_at` | TEXT | NOT NULL | |

Koneksi: `file:<path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate`; `db.SetMaxOpenConns(1)` (penulis tunggal, aman untuk demo). Transaksi stok memakai `BeginTx` (mode `IMMEDIATE` dari DSN).

### 3.1 Penyimpanan & Pencarian Vektor — keputusan teknis
`sqlite-vec` adalah ekstensi C yang **tidak bisa dimuat oleh `modernc.org/sqlite`** (pure-Go tanpa `load_extension`). Maka:
- **Default (wajib untuk v1):** vektor disimpan sebagai `BLOB` di tabel biasa; pencarian dilakukan **brute-force cosine di Go** (`vector.TopK`) — memadai untuk ≤ ±5.000 produk (latensi < 10 ms).
- Akses vektor disembunyikan di balik interface `vector.Store` (`Upsert`, `Delete`, `Search(queryVec, k, filter)`), sehingga implementasi `sqlite-vec` (via `ncruces/go-sqlite3` + `sqlite-vec-go-bindings/ncruces`, WASM, tetap tanpa CGO) atau **Qdrant lokal** dapat ditukar kemudian tanpa mengubah handler/kontrak API. Pilihan itu **di luar lingkup v1**.

## 4. Data Seed (idempoten, hanya bila `products` kosong)
| `id` | `name` | `category` | `price` | `stock` | Tujuan uji |
|---|---|---|---:|---:|---|
| `PRD-001` | Earbuds Nova X1 TWS | audio | 349000 | 50 | normal |
| `PRD-002` | Headphone Bass Pro H200 | audio | 799000 | 25 | normal |
| `PRD-003` | Powerbank 20000mAh FastCharge | aksesoris | 299000 | 80 | "powerbank <400rb" |
| `PRD-004` | Kabel USB-C to USB-C 1m 100W | aksesoris | 89000 | 200 | murah |
| `PRD-005` | Charger GaN 65W 3 Port | aksesoris | 249000 | 60 | normal |
| `PRD-006` | Smartwatch FitBand S5 | wearable | 899000 | 30 | normal |
| `PRD-007` | Keyboard Mekanikal K87 RGB | komputer | 649000 | 20 | order gagal (ORD-003) |
| `PRD-008` | Mouse Wireless Silent M330 | komputer | 159000 | 100 | mouse biasa (pembanding semantik) |
| `PRD-009` | Webcam Full HD 1080p | komputer | 329000 | 15 | normal |
| `PRD-010` | Speaker Bluetooth Boomer Mini | audio | 279000 | **0** | **stok habis** |
| `PRD-011` | Dudukan Smartphone Aluminium | aksesoris | 79000 | 150 | murah |
| `PRD-012` | SSD Eksternal 1TB | penyimpanan | 1299000 | **8** | **stok menipis**, ORD-004 |
| `PRD-013` | Tablet Lawas Seri A | komputer | 1500000 | 5 | **nonaktif** (`is_active=0`) |
| `PRD-014` | Mouse Vertikal Ergonomis ErgoGlide V2 | komputer | 289000 | 35 | **target uji semantik** "mouse yang tidak bikin pergelangan pegal" |

Setiap produk wajib memiliki `description` dan `specifications` Bahasa Indonesia yang kaya makna (kunci uji semantik). Contoh:
- `PRD-014` — description: "Mouse wireless dengan posisi genggaman vertikal seperti berjabat tangan, mengurangi tekanan pada pergelangan tangan sehingga tidak pegal dan nyaman dipakai berjam-jam." specifications: "Desain vertikal ergonomis 57°; sandaran ibu jari; DPI 800/1200/1600; Bluetooth + 2.4GHz; baterai 3 bulan."
- `PRD-008` — description: "Mouse wireless klik senyap untuk ruang kerja tenang." specifications: "Klik senyap; DPI 1000/1600; 2.4GHz; baterai AA 12 bulan; bentuk simetris standar."
- `PRD-003` — specifications: "Kapasitas 20000mAh; output 22.5W; 2×USB-A + 1×USB-C; layar indikator digital."
Seed menulis `stock_movements` reason `SEED`.

Definisi turunan: `stock_status` = `OUT_OF_STOCK` (stock=0), `LOW_STOCK` (1–10), `IN_STOCK` (>10).

## 5. Alur Embedding (generate vektor)

### 5.1 Teks yang diembedding (`embed.BuildText(p)`)
```
Nama: {name}
Kategori: {category}
Deskripsi: {description}
Spesifikasi: {specifications}
```
Tanpa harga/stok (data volatil tidak masuk vektor). `content_hash = sha256(text)`.

### 5.2 Kapan embedding dibuat
| Pemicu | Perilaku |
|---|---|
| **Seed** (start pertama) | Insert 14 produk → `indexer.IndexAll(ctx)` memakai **batch** `batchEmbedContents` (`taskType=RETRIEVAL_DOCUMENT`). Kegagalan embedding **tidak** menggagalkan start/seed |
| `POST /api/products`, `PUT /api/products/{id}` | Setelah commit DB: embedding sinkron dengan timeout `EMBEDDING_TIMEOUT_MS` (5 s). Gagal → produk tetap tersimpan (respons `201/200` normal); log warning; ditangani worker |
| Perubahan hanya `price/stock/is_active/image_url` | **Tidak** re-embed (hash teks sama) |
| **Worker** (goroutine, ticker 60 s) | Cari produk aktif yang tidak punya baris `product_embeddings`, atau `content_hash`/`model`/`dim` tidak cocok → embed ulang (maks 20 per siklus, backoff saat 429/5xx) |
| `POST /api/products/reindex` (ADMIN_SISTEM) | Paksa re-embed semua produk (abaikan hash) → `202 {"data":{"queued":14}}`; jalan di goroutine |
| `DELETE` (soft delete) | Baris embedding **dipertahankan**; pencarian memfilter `is_active=1` |

### 5.3 Klien Gemini (`internal/shared/embedding`)
| Aspek | Nilai |
|---|---|
| Endpoint | `POST https://generativelanguage.googleapis.com/v1beta/models/{EMBEDDING_MODEL}:embedContent` (tunggal) dan `:batchEmbedContents` (batch ≤ 100) |
| Auth | Header `x-goog-api-key: $GOOGLE_API_KEY` |
| Body | `{"content":{"parts":[{"text":"..."}]},"taskType":"RETRIEVAL_DOCUMENT"}`; untuk query `taskType:"RETRIEVAL_QUERY"` |
| Dimensi | Dibaca dari respons (`values`) dan divalidasi = `EMBEDDING_DIM` (default 768); jika model mendukung, kirim `outputDimensionality` |
| Pasca-proses | Normalisasi L2 sebelum disimpan (cosine = dot product) |
| Error | 429/5xx → retry maks 2× (backoff 300 ms, 900 ms); lainnya langsung gagal. Dibungkus `embedding.ErrUnavailable` |
| Cache query | LRU in-memory 256 entri (`q` ter-normalisasi → vektor) untuk menghemat panggilan |
| Konfigurasi | `GOOGLE_API_KEY`, `EMBEDDING_MODEL` (default `text-embedding-004`), `EMBEDDING_DIM` (768), `EMBEDDING_TIMEOUT_MS` (5000) |

> **Risiko ketersediaan model:** nama model dapat dihentikan Google. `EMBEDDING_MODEL` **wajib** configurable; pada start, service memeriksa `model`/`dim` di `product_embeddings` — bila berbeda dari konfigurasi, otomatis masuk antrean re-embed (jangan mencampur ruang vektor model berbeda dalam satu pencarian). Jika `text-embedding-004` sudah tidak tersedia, ganti ke `gemini-embedding-001` (set `EMBEDDING_DIM`) tanpa perubahan kode.

## 6. Kontrak REST API

Konvensi: Master §7 (`{"data":...}`, `{"data":[...],"meta":{...}}`, `{"error":{...}}`, `snake_case`). Representasi produk (`Product`):
```json
{
  "id": "PRD-003", "name": "Powerbank 20000mAh FastCharge", "description": "...",
  "specifications": "...", "category": "aksesoris", "price": 299000, "stock": 80,
  "stock_status": "IN_STOCK", "image_url": null, "is_active": true,
  "created_at": "2026-11-19T08:00:00.000Z", "updated_at": "2026-11-19T08:00:00.000Z"
}
```

| Method | Path | Role | Deskripsi |
|---|---|---|---|
| GET | `/api/products` | semua | Daftar/cari **eksak** (keyword SQL `LIKE`) |
| GET | `/api/products/semantic-search` | semua | **Pencarian semantik (vektor)** |
| GET | `/api/products/categories` | semua | Kategori + jumlah produk aktif |
| GET | `/api/products/{id}` | semua | Detail |
| POST | `/api/products` | ADMIN_SISTEM | Buat (+embedding) |
| PUT | `/api/products/{id}` | ADMIN_SISTEM | Ubah (+re-embed bila teks berubah) |
| DELETE | `/api/products/{id}` | ADMIN_SISTEM | Soft delete |
| PATCH | `/api/products/{id}/stock` | ADMIN_SISTEM | Penyesuaian stok manual |
| GET | `/api/products/{id}/stock-movements` | ADMIN_SISTEM, ADMIN_DEV | Riwayat stok |
| POST | `/api/products/reindex` | ADMIN_SISTEM | Re-embed semua produk |
| POST | `/internal/stock/decrement` | internal (`x-service-name: service-order`) | Kurangi stok atomik |
| POST | `/internal/stock/restock` | internal (`service-order`) | Kembalikan stok |
| GET | `/health` | publik | `{"data":{"status":"ok","service":"service-catalog"}}` |

### 6.1 `GET /api/products` (pencarian eksak)
| Query | Tipe | Default | Keterangan |
|---|---|---|---|
| `q` | string | — | `LIKE` case-insensitive pada `name`, `description`, `specifications` |
| `category` | string | — | exact (lowercase) |
| `min_price`, `max_price` | integer ≥ 0 | — | inklusif |
| `in_stock` | boolean | — | `true` → `stock > 0` |
| `sort` | `name`\|`price_asc`\|`price_desc`\|`newest` | `name` | |
| `page`, `limit` | integer | 1, 20 (maks 100) | |
| `include_inactive` | boolean | false | hanya dihormati untuk ADMIN_* |

`200 { "data": [Product...], "meta": { "total": 1, "page": 1, "limit": 20 } }`. `LIKE` memakai parameter bind & escape `%`/`_`.

### 6.2 `GET /api/products/semantic-search`
Mencari produk berdasarkan **makna** kebutuhan/keluhan pengguna.

| Query | Tipe | Default | Validasi |
|---|---|---|---|
| `q` | string | — | **wajib**, 3–300 karakter (setelah `TrimSpace`) |
| `limit` | integer | 5 | 1–20 |
| `category` | string | — | filter pasca-pencarian |
| `max_price` / `min_price` | integer | — | filter pasca-pencarian |
| `in_stock` | boolean | — | `true` → hanya `stock > 0` |
| `min_score` | float 0–1 | `SEMANTIC_MIN_SCORE` (0.55) | ambang cosine; hasil di bawahnya dibuang |

Algoritma:
```
1. Validasi q.
2. qv = embedding.Embed(q, taskType=RETRIEVAL_QUERY)   (cache LRU)
3. Muat vektor produk aktif (cache in-memory, di-invalidate saat embedding berubah)
4. skor = dot(qv, v)  → urutkan menurun → terapkan filter (category/harga/in_stock/is_active) → ambil top `limit` dengan skor ≥ min_score
5. Gabungkan data produk terkini (harga & stok langsung dari tabel products)
```
Respons `200`:
```json
{
  "data": [
    { "id": "PRD-014", "name": "Mouse Vertikal Ergonomis ErgoGlide V2", "description": "...", "specifications": "...",
      "category": "komputer", "price": 289000, "stock": 35, "stock_status": "IN_STOCK",
      "image_url": null, "is_active": true, "created_at": "...", "updated_at": "...",
      "score": 0.8123 }
  ],
  "meta": { "total": 1, "limit": 5, "query": "mouse yang tidak bikin pergelangan pegal", "mode": "semantic", "min_score": 0.55 }
}
```
| `meta.mode` | Arti |
|---|---|
| `semantic` | Hasil dari vektor |
| `keyword_fallback` | Embedding API tak tersedia/ belum terindeks → fallback ke `LIKE` per kata kunci (`score: null`). Header `x-search-mode: keyword_fallback` ikut dikirim |

| HTTP | `code` | Kondisi |
|---|---|---|
| 400 | `VALIDATION_ERROR` | `q` kosong/terlalu pendek/panjang; `min_score` di luar 0–1 |
| 503 | `EMBEDDING_UNAVAILABLE` | Hanya bila fallback keyword juga tidak mungkin (tidak diharapkan) |

`score` ∈ [0,1] dibulatkan 4 desimal; hasil kosong → `data: []` (`200`). Kontrak yang sama dikutip MCP (`05-SERVICE-MCP.md`).

### 6.3 `GET /api/products/{id}`
`200 { "data": Product }`. Tidak ada, atau nonaktif untuk non-admin → `404 NOT_FOUND`.

### 6.4 `POST /api/products` (ADMIN_SISTEM)
| Field | Tipe | Validasi |
|---|---|---|
| `name` | string | wajib, 3–120 |
| `description` | string | opsional, ≤ 1000 |
| `specifications` | string | opsional, ≤ 1000 |
| `category` | string | wajib, `[a-z0-9-]{2,30}` |
| `price` | integer | wajib, ≥ 0 |
| `stock` | integer | wajib, ≥ 0 |
| `image_url` | string (URL) | opsional |

`201 { "data": Product }` (+ `stock_movements` `ADMIN_ADJUST` bila stock > 0) lalu embedding (§5.2). Validasi dengan helper `validate` (kumpulkan semua error → `details: [{field, message}]`).

### 6.5 `PUT /api/products/{id}` (ADMIN_SISTEM)
Field sama dengan POST **kecuali `stock`**, ditambah `is_active` (boolean). `200 { "data": Product }`; tidak ada → `404`. Re-embed bila `content_hash` berubah.

### 6.6 `DELETE /api/products/{id}` (ADMIN_SISTEM)
Soft delete → `is_active=0`. `204` tanpa body. Idempoten.

### 6.7 `PATCH /api/products/{id}/stock` (ADMIN_SISTEM)
`{ "mode": "set" | "adjust", "value": 25, "note": "Stok opname" }` — `set`: stok = value (≥0); `adjust`: stok += value (boleh negatif); hasil < 0 → `409 INSUFFICIENT_STOCK`. `200 { "data": Product }` + `stock_movements` `ADMIN_ADJUST`.

### 6.8 `POST /api/products/reindex` (ADMIN_SISTEM)
`202 { "data": { "queued": 14 } }`. Idempoten; bila reindex sedang berjalan → `409 CONFLICT`.

### 6.9 `POST /internal/stock/decrement`
Dipanggil `service-order` (HTTP client native Go). **Atomik** (`BeginTx`; semua item berhasil atau tidak sama sekali).
```json
{ "reference_id": "ORD-009",
  "items": [ { "product_id": "PRD-003", "quantity": 1 }, { "product_id": "PRD-004", "quantity": 2 } ] }
```
| Validasi | Error |
|---|---|
| `items` 1–20, `quantity` 1–99, `product_id` unik | `400 VALIDATION_ERROR` |
| Produk tidak ada | `404 NOT_FOUND` (`details.product_id`) |
| `is_active=0` | `422 PRODUCT_INACTIVE` |
| `stock < quantity` | `409 INSUFFICIENT_STOCK` (`details: {product_id, available, requested}`) |

Sukses `200`:
```json
{ "data": { "reference_id": "ORD-009",
  "items": [ { "product_id": "PRD-003", "name": "...", "unit_price": 299000, "quantity": 1, "stock_after": 79 } ] } }
```
Menulis `stock_movements` `ORDER_RESERVE`. **Harga/nama dari sini** menjadi snapshot order (klien tak dipercaya).

### 6.10 `POST /internal/stock/restock`
`{ "reference_id": "ORD-009", "reason": "PAYMENT_TIMEOUT", "items": [ {"product_id":"PRD-003","quantity":1} ] }`
Idempoten per `(reference_id, product_id)` untuk `ORDER_RESTOCK`. `200 { "data": { "reference_id": "ORD-009", "restocked": true } }` (`false` bila sudah pernah).

## 7. Aturan Visibilitas & RBAC Data
- GUEST/CUSTOMER hanya melihat `is_active=1` (termasuk di `semantic-search`).
- Mutasi katalog hanya `ADMIN_SISTEM`; `ADMIN_DEV` read-only.
- `/internal/*` tak terjangkau dari Gateway; defence in depth lewat `x-service-name`.

## 8. Bootstrap `main.go`
1. `config.Load()` (wajib: `INTERNAL_API_KEY`; `GOOGLE_API_KEY` **opsional** — tanpa itu service tetap hidup, semantic-search memakai `keyword_fallback` dan log warning).
2. `sqlitex.Open` → migrate → seed.
3. Start `indexer.Worker(ctx)` (goroutine) dan `IndexAll` awal (non-blocking).
4. `http.Server{Addr: ":"+CATALOG_PORT, ReadHeaderTimeout: 5s}`; `signal.NotifyContext` → `Shutdown` + hentikan worker.

## 9. Kriteria Penerimaan
- [ ] `CGO_ENABLED=0 go build ./service-catalog` sukses.
- [ ] Tanpa/salah `x-internal-api-key` → `401 INVALID_INTERNAL_KEY` (kecuali `/health`).
- [ ] `GET /api/products?q=powerbank&max_price=400000` → hanya `PRD-003`.
- [ ] `GET /api/products/semantic-search?q=mouse yang tidak bikin pergelangan pegal` → `PRD-014` di peringkat 1 dengan `score` lebih tinggi daripada `PRD-008`; `meta.mode="semantic"`.
- [ ] `semantic-search?q=ab` → `400`; `q` tanpa kata yang relevan (mis. "resep rendang") → `data: []`.
- [ ] Tanpa `GOOGLE_API_KEY`: seed tetap sukses, semantic-search → `meta.mode="keyword_fallback"`.
- [ ] Produk baru lewat `POST` langsung dapat ditemukan semantik dalam ≤ 1 siklus worker bila embedding sinkron gagal.
- [ ] Ubah hanya `price` → tidak ada panggilan embedding baru.
- [ ] CUSTOMER `POST /api/products` → `403`; `PRD-013` (nonaktif) tidak muncul untuk non-admin di kedua jenis pencarian.
- [ ] Decrement dengan 1 item kurang stok → tidak ada stok berubah; restock dua kali → bertambah sekali.
- [ ] `/internal/*` tanpa `x-service-name: service-order` → `403`.
