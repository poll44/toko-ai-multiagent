# 00 — MASTER TRD: `toko-ai-microservices`

| Item | Nilai |
|---|---|
| Versi dokumen | 2.0 (backend Go native + Vector Search/RAG + Nuxt SPA) |
| Status | Siap implementasi |
| Dokumen produk | [`docs/prd/`](../prd/00-MASTER-PRD.md) — kebutuhan produk (apa dan mengapa); TRD ini menjawab *bagaimana* |
| Dokumen turunan | `01-SERVICE-GATEWAY` … `07-SERVICE-FRONTEND` |
| Catatan | Dokumen ini adalah **sumber kebenaran tunggal** untuk nama header, port, konvensi JSON, kode error, dan matriks RBAC. Jika ada konflik, dokumen ini menang. |

### Ringkasan perubahan v2.0
1. `gateway`, `catalog`, `order`, `ticket` dimigrasi dari Express/TypeScript ke **Go native `net/http`** (`http.ServeMux` Go ≥ 1.22, tanpa web framework) + `modernc.org/sqlite` (pure-Go, tanpa CGO).
2. Ditambah **Vector Search & RAG**: pencarian semantik produk (`service-catalog`) dan knowledge base kebijakan toko (`service-ticket`) memakai Gemini Embedding.
3. Path API domain distandarkan berawalan `/api/...` di **semua** lapisan (browser → gateway → service). Gateway tidak lagi menulis ulang path domain.
4. `service-frontend` = Nuxt 3 **SPA** (`ssr: false`), JWT di `localStorage`, memanggil Gateway langsung.
5. `service-mcp` dan `service-agent` **juga dimigrasi ke Go** (SDK MCP resmi `github.com/modelcontextprotocol/go-sdk` dan `google.golang.org/adk` v1.x); ditambah tool `semantic_search_products` dan `search_store_policy` (total **17 tool**). Satu-satunya service TypeScript tersisa: `service-frontend` (Nuxt).

---

## 1. Visi Produk

`toko-ai-microservices` adalah toko e-commerce elektronik & aksesoris berbasis **microservices** dengan **asisten belanja & layanan pelanggan Multi-Agent AI**. Pelanggan dapat mencari produk (termasuk dengan bahasa natural/keluhan), memeriksa stok, membuat pesanan, mengecek status pembayaran, bertanya kebijakan toko, dan membuat tiket komplain lewat percakapan.

### Tujuan
1. Arsitektur microservices dengan **zero-trust antar-service** dan **autentikasi terpusat di gateway**.
2. **1 Orchestrator + 2 Sub-Agent** (Google ADK) yang memakai **MCP Tools**, dengan **identitas user tetap terbawa** sampai service domain.
3. **Pemahaman semantik** (bukan hanya `LIKE`) untuk produk dan kebijakan toko, dengan jawaban kebijakan yang berbasis dokumen (RAG) — bukan karangan model.
4. Data seed yang cukup (termasuk pesanan gagal/timeout) untuk menguji skenario AI.

### Non-Tujuan
- Pembayaran/pengiriman nyata, multi-tenant, HA/scaling produksi.
- Keamanan produksi: password plaintext `123456` **hanya demo**.

### Metrik Keberhasilan
| Metrik | Target |
|---|---|
| Semua service hidup | 7/7 |
| Build backend Go dengan `CGO_ENABLED=0` | sukses untuk 6 service |
| Request tanpa `x-internal-api-key` ke service domain | 100% `401` |
| CUSTOMER mengakses data user lain | 0 kebocoran (`404`) |
| Query semantik uji ("mouse yang tidak bikin pergelangan pegal") | `PRD-014` peringkat 1 |
| Jawaban kebijakan tanpa memanggil `search_store_policy` | 0 kasus |
| Agent menjawab data order/stok tanpa tool | 0 kasus |

---

## 2. Topologi, Port & Tech Stack

| Service | Port | Stack | Database | Dipanggil oleh |
|---|---|---|---|---|
| `service-frontend` | `3000` | Nuxt 3 **SPA (`ssr: false`)** + TypeScript + Tailwind CSS | — (`localStorage` JWT) | Browser |
| `service-gateway` | `8080` | **Go `net/http`**, `httputil.NewSingleHostReverseProxy`, `golang-jwt/jwt/v5`, `modernc.org/sqlite` | `auth.db` | Browser (Frontend) |
| `service-catalog` | `4001` | **Go `net/http`**, `modernc.org/sqlite`, Gemini Embedding | `catalog.db` (+ vektor produk) | Gateway, `service-order`, `service-mcp` |
| `service-order` | `4002` | **Go `net/http`**, `modernc.org/sqlite` | `order.db` | Gateway, `service-ticket`, `service-mcp` |
| `service-ticket` | `4003` | **Go `net/http`**, `modernc.org/sqlite`, Gemini Embedding | `ticket.db` (+ knowledge base & vektor) | Gateway, `service-mcp` |
| `service-mcp` | `5000` | **Go** + `go-sdk/mcp` Streamable HTTP **stateless** (atau Stdio untuk dev), tanpa DB | — | `service-agent` |
| `service-agent` | `8000` | **Go** + Google ADK (`google.golang.org/adk` v1.9, `server/adkrest`) | in-memory session (dev) | Gateway |

Persyaratan runtime: **Go ≥ 1.22** secara fitur (pola `ServeMux` `METHOD /path/{param}` & `r.PathValue`), tetapi `go.mod` saat ini `go 1.26.0` karena `modernc.org/sqlite v1.62.0` mensyaratkannya — gunakan toolchain Go ≥ 1.26 (atau pin versi `modernc.org/sqlite` yang lebih lama bila perlu toolchain lebih rendah). **Node.js ≥ 20** hanya untuk `service-frontend`. Tidak ada CGO untuk seluruh backend Go (6 service).

---

## 3. Diagram Arsitektur

### 3.1 Komunikasi antar-service
```
                    ┌────────────────────────────┐
                    │ Browser — Nuxt 3 SPA :3000 │  JWT di localStorage
                    │  RoleSwitcher + Chat + UI  │
                    └─────────────┬──────────────┘
                                  │ http://localhost:8080  Authorization: Bearer <JWT>  (CORS)
                                  ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ service-gateway :8080 (Go)   — satu-satunya verifikasi JWT, auth.db          │
│ /auth/*   /api/products/**  /api/orders/**  /api/tickets/**  /api/knowledge/**│
│ /api/agent/** (strip prefix + stripBrowserOrigin + identityRewrite)          │
│ inject: x-user-id, x-user-role, x-internal-api-key, x-request-id             │
└───────┬──────────────┬───────────────┬──────────────────────┬────────────────┘
        ▼              ▼               ▼                      ▼
 ┌─────────────┐ ┌─────────────┐ ┌───────────────────┐  ┌──────────────────────┐
 │ catalog     │◄│ order       │ │ ticket            │  │ service-agent :8000  │
 │ :4001  (Go) │ │ :4002  (Go) │◄│ :4003  (Go)       │  │ Orchestrator         │
 │ catalog.db  │ │ order.db    │ │ ticket.db         │  │  ├ Shopping&Catalog  │
 │ + vektor    │ │             │ │ + KB + vektor     │  │  └ Order&Support     │
 │   produk    │ │             │ │   kebijakan toko  │  └──────────┬───────────┘
 └──▲───▲──────┘ └─────────────┘ └──▲──────▲─────────┘             │ MCP (HTTP)
    │   │ stok (internal)            │      │ validasi order        ▼
    │   │                            │      └────► order     ┌───────────────────┐
    │   │ embed (Gemini)             │ embed (Gemini)        │ service-mcp :5000 │
    │   └────────────┐   ┌───────────┘                       │ 17 MCP Tools      │
    │                ▼   ▼                                   └─────────┬─────────┘
    │          ┌──────────────────┐        REST + header identitas     │
    │          │ Gemini Embedding │◄───────────────────────────────────┘ (tidak langsung;
    │          │ API (text-emb…)  │                                      hanya catalog/ticket)
    └──────────┴──────────────────┘
```

### 3.2 Alur request terautentikasi (CUSTOMER)
```
Browser ──GET /api/orders (Bearer JWT)──► Gateway
Gateway: verify JWT ✔ → hapus x-user-*/x-internal-api-key klien → set:
         x-user-id: USR-01 | x-user-role: CUSTOMER | x-internal-api-key: <key> | x-request-id: <uuid>
         → hapus Authorization → ReverseProxy
Gateway ──GET /api/orders──► service-order
service-order: InternalAuthMiddleware ✔ → role CUSTOMER → WHERE user_id='USR-01' → 200 {"data":[...]}
```

### 3.3 Alur request AI Agent
```
Browser ─POST /api/agent/run (Bearer JWT)─► Gateway
Gateway: verify JWT (GUEST boleh) → inject header → body.userId == x-user-id? (else 403)
         → saat create session: state{user_id,user_role} → strip prefix /api/agent
         → hapus Origin (stripBrowserOrigin) ─► service-agent :8000
service-agent: session.state → Orchestrator → Sub-Agent
Sub-Agent ─MCP (x-user-id, x-user-role, x-internal-api-key)─► service-mcp :5000
service-mcp ─REST (+identitas, x-service-name: service-mcp)─► catalog / order / ticket
JSON ─► Sub-Agent ─► jawaban ─► Gateway ─► Browser
```

### 3.4 Alur Vector Search / RAG
```
(A) INDEXING                                        (B) QUERY
catalog: seed/POST/PUT produk                       User: "mouse yang tidak bikin pergelangan pegal"
ticket : seed/POST/PUT dokumen KB → chunking          │  Agent (Shopping) → tool semantic_search_products
   │  teks = nama+kategori+deskripsi+spesifikasi      ▼
   │         / judul+chunk kebijakan              MCP → GET /api/products/semantic-search?q=...
   ▼                                                   │ catalog: embed(q, RETRIEVAL_QUERY) → cosine vs product_embeddings
Gemini Embedding (RETRIEVAL_DOCUMENT)                  │ filter aktif/harga/stok → top-K (score ≥ min_score)
   │ vektor float32, L2-normalised                     ▼
   ▼                                              data[{...Product, score}] ─► Agent: verifikasi stok/harga, jawab
tabel *_embeddings (BLOB)                         Pertanyaan kebijakan → search_store_policy → GET /api/knowledge/search
   (hash isi → skip re-embed; worker 60 s retry)      → chunk kebijakan (kutipan) → Agent menjawab berbasis kutipan
```

---

## 4. Struktur Monorepo & Shared Package (Go)

Satu **Go module** di root agar semua service Go berbagi kode lewat `internal/shared` (aturan `internal` Go mengizinkan seluruh pohon module).

```
toko-ai-microservices/
├── go.mod                       # module toko-ai-microservices ; go 1.22
├── go.sum
├── .env.example  .gitignore
├── docs/prd/
├── internal/shared/             # ===== shared helper (dipakai 6 service Go) =====
│   ├── httputil/                # response, request, middleware, identity, errors
│   ├── sqlitex/                 # Open(), WithTx(), Now()
│   ├── validate/                # validator ringan tanpa framework
│   ├── svcclient/               # HTTP client service-ke-service + propagasi identitas
│   ├── embedding/               # klien Gemini Embedding + cache LRU
│   ├── vector/                  # Encode/Decode float32, Normalize, Cosine, TopK, interface Store
│   └── config/                  # helper env: String, Int, Duration, Require
├── service-gateway/   {main.go, internal/...}     # go run ./service-gateway
├── service-catalog/   {main.go, internal/...}
├── service-order/     {main.go, internal/...}
├── service-ticket/    {main.go, internal/...}
├── service-mcp/       {main.go, internal/...}   # Go — go-sdk MCP, stateless
├── service-agent/     {main.go, internal/...}   # Go — Google ADK + idguard
└── service-frontend/  # Nuxt 3 SPA (package.json)
```
Build: `CGO_ENABLED=0 go build -o bin/ ./service-gateway ./service-catalog ./service-order ./service-ticket ./service-mcp ./service-agent`. Dependensi eksternal Go **dibatasi**: `modernc.org/sqlite` (gateway/catalog/order/ticket), `github.com/golang-jwt/jwt/v5` (gateway saja), `github.com/modelcontextprotocol/go-sdk` (mcp, agent), `google.golang.org/adk` + `google.golang.org/genai` (agent). Selain itu hanya standard library (UUID via `crypto/rand`, LRU sederhana buatan sendiri, `sync.WaitGroup` untuk fan-out).

### 4.1 `internal/shared/httputil` — kontrak helper
Tujuan: menghilangkan boilerplate berulang; **seluruh handler hanya memakai helper ini untuk I/O**.

| Fungsi / tipe | Signature | Perilaku |
|---|---|---|
| `WriteJSON` | `WriteJSON(w http.ResponseWriter, status int, v any)` | Set `Content-Type: application/json; charset=utf-8`, `WriteHeader(status)`, `json.NewEncoder(w).Encode(v)`; `204` tanpa body |
| `WriteData` | `WriteData(w, status, data any)` | `WriteJSON(w, status, map[string]any{"data": data})` |
| `WriteList` | `WriteList(w, data any, meta Meta)` | `{"data":[...],"meta":{...}}` (`200`) |
| `WriteError` | `WriteError(w, r, status int, code, message string, details any)` | `{"error":{"code","message","details","request_id"}}`; `request_id` dari context |
| `ReadJSON` | `ReadJSON(w, r, dst any) error` | `http.MaxBytesReader` (1 MB), wajib `Content-Type: application/json`, decode, tolak body ganda/`trailing data`; **tidak** `DisallowUnknownFields` secara default (opsi `ReadJSONStrict`). Error → `*APIError{400 VALIDATION_ERROR}` yang sudah siap `WriteError` |
| `InternalAuthMiddleware` | `InternalAuthMiddleware(key string) func(http.Handler) http.Handler` | `subtle.ConstantTimeCompare` header `x-internal-api-key`; gagal → `401 INVALID_INTERNAL_KEY` |
| `IdentityMiddleware` | `IdentityMiddleware() func(http.Handler) http.Handler` | Parse `x-user-id`/`x-user-role` (+`x-service-name`), validasi role, simpan `Identity` di `context`; invalid → `403 FORBIDDEN` |
| `IdentityFrom` | `IdentityFrom(ctx) Identity` | `Identity{UserID, Role, RequestID, ServiceName string}` |
| `RequireRole` | `RequireRole(roles ...string) func(http.Handler) http.Handler` | `403 FORBIDDEN` bila role tak termasuk |
| `DenyGuest` | `DenyGuest(next http.Handler) http.Handler` | `GUEST` → `403 FORBIDDEN` |
| `RequireServiceCaller` | `RequireServiceCaller(name string) …` | `x-service-name` harus sama, else `403` |
| `RequestID` | middleware | Pakai `x-request-id` valid atau buat UUID v4; simpan di context & header respons |
| `Recover` | middleware | `recover()` → `500 INTERNAL_ERROR`; log stack |
| `Logging` | middleware | `log/slog` JSON: method, path, status, ms, request_id, user_id (tanpa secret) |
| `Chain` | `Chain(mw ...func(http.Handler) http.Handler) func(http.Handler) http.Handler` | Komposisi middleware |
| `Meta` | `struct{Total, Page, Limit int; ...}` | Meta daftar; field tambahan via `Extra map[string]any` |
| `APIError` | `struct{Status int; Code, Message string; Details any}` | Implement `error`; handler `if err != nil { httputil.Fail(w, r, err); return }` (`Fail` memetakan `*APIError` / error lain → 500) |

Pola handler standar (semua service):
```go
func (h *Handlers) GetProduct(w http.ResponseWriter, r *http.Request) {
    p, err := h.repo.Get(r.Context(), r.PathValue("id"), httputil.IdentityFrom(r.Context()))
    if err != nil { httputil.Fail(w, r, err); return }
    httputil.WriteData(w, http.StatusOK, p)
}
```

### 4.2 Paket shared lain
| Paket | Isi wajib |
|---|---|
| `sqlitex` | `Open(path string) (*sql.DB, error)` — DSN `file:<path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate`, driver `"sqlite"`, `SetMaxOpenConns(1)`; `WithTx(ctx, db, func(*sql.Tx) error) error` (commit/rollback otomatis); `NowUTC() string` (RFC 3339 ms, `Z`) |
| `validate` | `V := validate.New(); V.Required("name", s); V.Len("name", s, 3, 120); V.Int("price", n, 0, max); V.OneOf("role", s, ...); V.Regex(...)`; `V.Err() *httputil.APIError` (`VALIDATION_ERROR` + `details:[{field,message}]`) |
| `svcclient` | `New(base, key, serviceName string, timeout time.Duration) *Client`; `Do(ctx, method, path string, in, out any) error` — menyalin `x-user-id`, `x-user-role`, `x-request-id` dari `IdentityFrom(ctx)`, set `x-internal-api-key`, `x-service-name`; `*UpstreamError{Status,Code,Message,Details}` |
| `embedding` | `NewGemini(apiKey, model string, dim int, timeout) *Client`; `Embed(ctx, text string, task Task) ([]float32, error)`; `EmbedBatch(ctx, texts []string, task Task)`; `ErrUnavailable`; cache LRU untuk query |
| `vector` | `Normalize`, `Cosine/Dot`, `Encode([]float32) []byte`, `Decode([]byte) []float32`, `TopK(query, items, k)`; `type Store interface{ Upsert; Delete; Search }` |
| `config` | `Require("INTERNAL_API_KEY")` (fail-fast), `String/Int/Duration` dengan default |

---

## 5. Aturan Autentikasi & Otorisasi (Berlaku Global)

1. **Stateless JWT di Gateway.** Hanya `service-gateway` memverifikasi `Authorization: Bearer <token>` dan memiliki `auth.db`. Service lain **tidak** melihat JWT.
2. **Role:** `GUEST` (tanpa token), `CUSTOMER`, `ADMIN_SISTEM`, `ADMIN_DEV`.
3. **Header identitas** (disuntik Gateway ke semua request proxy):

| Header | Nilai | Catatan |
|---|---|---|
| `x-user-id` | `USR-01`, `USR-02`, `ADM-01`, `DEV-01`, atau `GUEST` | `GUEST` bila tanpa token |
| `x-user-role` | `GUEST` \| `CUSTOMER` \| `ADMIN_SISTEM` \| `ADMIN_DEV` | |
| `x-internal-api-key` | nilai `INTERNAL_API_KEY` | rahasia bersama |
| `x-request-id` | UUID v4 | tracing |
| `x-service-name` | mis. `service-order` | Bukan dari Gateway; hanya service-ke-service/MCP; membatasi `/internal/*` |

   Catatan Go: header HTTP case-insensitive; Go mengkanonikalkan menjadi `X-User-Id` dll. Selalu baca/tulis dengan `r.Header.Get/Set("x-user-id")`.
4. **Sanitasi:** Gateway menghapus `x-user-id`, `x-user-role`, `x-internal-api-key`, `x-service-name` dari klien sebelum menyuntik nilai sendiri.
5. **Zero-Trust:** `catalog`, `order`, `ticket` menolak `401 INVALID_INTERNAL_KEY` bila key salah (kecuali `GET /health`), lalu memfilter data memakai `x-user-id` + `x-user-role`. `service-mcp` juga memvalidasi key dari `service-agent`.
6. **`/internal/*`** hanya bila `x-service-name` ∈ allowlist; Gateway memblokir segmen `internal` (404).
7. **Agent:** Gateway menghapus header `Origin` (`stripBrowserOrigin`) pada `/api/agent` dan menghapus prefiks `/api/agent`.
8. **Demo users** (seed `auth.db`, password plaintext `123456` — demo):

| `id` | `email` | `name` | `role` |
|---|---|---|---|
| `USR-01` | `budi@toko.com` | Budi Santoso | `CUSTOMER` |
| `USR-02` | `siti@toko.com` | Siti Rahma | `CUSTOMER` |
| `ADM-01` | `admin@toko.com` | Admin Sistem | `ADMIN_SISTEM` |
| `DEV-01` | `dev@toko.com` | Developer Toko | `ADMIN_DEV` |

---

## 6. Matriks RBAC Lintas Service

Legenda: ✅ penuh · 👁 hanya milik sendiri · 📖 baca semua · ❌ ditolak.

| Resource / Aksi | GUEST | CUSTOMER | ADMIN_SISTEM | ADMIN_DEV |
|---|:-:|:-:|:-:|:-:|
| `POST /auth/login`, `GET /auth/demo-users` | ✅ | ✅ | ✅ | ✅ |
| Catalog: `GET /api/products`, `/{id}`, `/categories` | ✅ | ✅ | ✅ | ✅ |
| Catalog: `GET /api/products/semantic-search` | ✅ | ✅ | ✅ | ✅ |
| Catalog: lihat produk nonaktif | ❌ | ❌ | ✅ | ✅ |
| Catalog: buat/ubah/hapus produk, atur stok, `reindex` | ❌ | ❌ | ✅ | ❌ |
| Order: buat order | ❌ | ✅ | ❌ | ❌ |
| Order: lihat daftar/detail | ❌ | 👁 | 📖 | 📖 |
| Order: bayar / batalkan | ❌ | 👁 | ✅ (cancel) | ❌ |
| Order: ubah status fulfilment | ❌ | ❌ | ✅ | ❌ |
| Ticket: buat tiket | ❌ | ✅ | ❌ | ❌ |
| Ticket: lihat daftar/detail | ❌ | 👁 | 📖 | 📖 |
| Ticket: tambah pesan | ❌ | 👁 | ✅ | ✅ (hanya `TECHNICAL`) |
| Ticket: ubah status | ❌ | 👁 (reopen & close) | ✅ | ✅ (hanya `TECHNICAL`) |
| Knowledge: `GET /api/knowledge/search`, `/documents`, `/documents/{id}` | ✅ | ✅ | ✅ | ✅ |
| Knowledge: buat/ubah/hapus dokumen, `reindex` | ❌ | ❌ | ✅ | ❌ |
| Agent: chat (`/api/agent/**`) | ✅ (tool katalog & kebijakan) | ✅ | ✅ | ✅ |
| Gateway: `GET /api/dev/health` | ❌ | ❌ | ❌ | ✅ |

Pelanggaran **role** → `403 FORBIDDEN`. Akses **resource milik user lain** oleh CUSTOMER → `404 NOT_FOUND`. Tanpa token pada route yang butuh login → Gateway `401 UNAUTHENTICATED` (order/ticket); `GUEST` yang lolos ke order/ticket (mis. via MCP) → `403`.

---

## 7. Konvensi Kontrak JSON (Wajib Seragam: Go ↔ MCP ↔ Nuxt)

### 7.1 Aturan umum
| Aspek | Aturan |
|---|---|
| Nama field | **`snake_case` di seluruh API kita** (request, response, `meta`, `error`). Struct Go: `json:"snake_case"`. **Satu-satunya pengecualian:** payload protokol ADK di jalur `/api/agent/**` (`appName`, `userId`, `sessionId`, `newMessage`, `stateDelta`, dll. — camelCase milik ADK) |
| Sukses objek | `{ "data": {...} }` |
| Sukses daftar | `{ "data": [...], "meta": { "total": 12, "page": 1, "limit": 20 } }` (meta dapat memuat field tambahan, mis. `mode`, `query`, `min_score`) |
| Sukses tanpa isi | `204` tanpa body (DELETE) |
| Error | `{ "error": { "code", "message", "details", "request_id" } }` |
| `null` | Field nullable **selalu ada** dengan nilai `null` (tanpa `omitempty`); array kosong `[]` bukan `null` |
| Waktu | RFC 3339 UTC milidetik, mis. `2026-11-19T08:30:00.000Z` |
| Uang | Rupiah **integer** (`price`, `unit_price`, `subtotal`, `total_amount`) |
| Boolean | JSON `true/false` (di SQLite `INTEGER 0/1`) |
| ID | String berprefiks: `USR-01`, `PRD-001`, `ORD-001`, `TKT-001`, `KB-001` |
| Encoding | `Content-Type: application/json; charset=utf-8` |
| Health | `GET /health` → `200 {"data":{"status":"ok","service":"service-xxx"}}` (tanpa internal key) |
| Path domain | Semua berawalan `/api/...` pada browser, Gateway, dan service (identik); path internal `/internal/...` |

### 7.2 Format Error
```json
{
  "error": {
    "code": "INSUFFICIENT_STOCK",
    "message": "Stok produk PRD-012 tidak mencukupi.",
    "details": { "product_id": "PRD-012", "available": 8, "requested": 10 },
    "request_id": "7c9e6679-7425-40de-944b-e07fc1f90ae7"
  }
}
```
| Field | Tipe | Wajib | Keterangan |
|---|---|:-:|---|
| `error.code` | string `UPPER_SNAKE` | ✅ | Stabil; dipakai klien & agent |
| `error.message` | string (Bahasa Indonesia) | ✅ | Aman ditampilkan |
| `error.details` | object \| array \| null | ✅ (boleh `null`) | Validasi: `[{field, message}]` |
| `error.request_id` | string | ✅ | Dari `x-request-id` |

### 7.3 Katalog kode error & status HTTP
| HTTP | `code` | Kapan |
|---|---|---|
| 400 | `VALIDATION_ERROR` | Body/query/param tidak valid |
| 401 | `UNAUTHENTICATED` | Token tidak ada/invalid/kedaluwarsa (Gateway) |
| 401 | `INVALID_CREDENTIALS` | Login gagal |
| 401 | `INVALID_INTERNAL_KEY` | `x-internal-api-key` salah/hilang |
| 403 | `FORBIDDEN` | Role tidak berhak |
| 404 | `NOT_FOUND` | Tidak ada / bukan milik user |
| 405 | `METHOD_NOT_ALLOWED` | Method tak sesuai pola |
| 409 | `INSUFFICIENT_STOCK` | Stok kurang |
| 409 | `INVALID_STATE_TRANSITION` | Transisi status tak sah |
| 409 | `CONFLICT` | Duplikat/konflik |
| 422 | `PRODUCT_INACTIVE` | Produk nonaktif dipesan |
| 429 | `RATE_LIMITED` | (opsional) |
| 500 | `INTERNAL_ERROR` | Tak terduga (tanpa stack ke klien) |
| 502 | `UPSTREAM_ERROR` | Service tujuan error/tak merespons |
| 503 | `EMBEDDING_UNAVAILABLE` | Embedding gagal **dan** fallback keyword tidak mungkin |
| 504 | `UPSTREAM_TIMEOUT` | Timeout ke service tujuan |

---

## 8. Manajemen Environment Variables

Satu `.env.example` di root; tiap service membaca variabelnya (Go: `os.Getenv` via `shared/config`, dimuat dengan `--env-file`/`direnv`/skrip; Node: `dotenv`/`--env-file`).

| Variabel | Default dev | Dipakai oleh | Keterangan |
|---|---|---|---|
| `JWT_SECRET` | `dev-only-change-me` | gateway | HS256; hanya gateway |
| `JWT_EXPIRES_IN` | `8h` | gateway | `time.ParseDuration` |
| `INTERNAL_API_KEY` | `internal-dev-key-123` | gateway, catalog, order, ticket, mcp, agent | rahasia bersama |
| `GATEWAY_PORT` / `CATALOG_PORT` / `ORDER_PORT` / `TICKET_PORT` | `8080` / `4001` / `4002` / `4003` | masing-masing | |
| `MCP_PORT` / `AGENT_PORT` / `FRONTEND_PORT` | `5000` / `8000` / `3000` | masing-masing | |
| `CATALOG_URL` | `http://localhost:4001` | gateway, order, mcp | |
| `ORDER_URL` | `http://localhost:4002` | gateway, ticket, mcp | |
| `TICKET_URL` | `http://localhost:4003` | gateway, mcp | |
| `MCP_URL` | `http://localhost:5000/mcp` | agent | |
| `AGENT_URL` | `http://localhost:8000` | gateway | |
| `MCP_BASE_URL` | `http://localhost:5000` | gateway | Basis URL MCP untuk `GET /health` pada `/api/dev/health` (`MCP_URL` memuat path `/mcp`) |
| `AGENT_TIMEOUT_MS` | `120000` | gateway | Timeout proxy ke agent (LLM lambat) |
| `MCP_DEV_USER_ID` / `MCP_DEV_USER_ROLE` | `GUEST` / `GUEST` | mcp | Hanya mode stdio dev |
| `CORS_ORIGIN` | `http://localhost:3000` | gateway | |
| `AUTH_DB_PATH` / `CATALOG_DB_PATH` / `ORDER_DB_PATH` / `TICKET_DB_PATH` | `./data/auth.db` dst. | masing-masing | |
| `ORDER_PAYMENT_TTL_MINUTES` | `15` | order | |
| `UPSTREAM_TIMEOUT_MS` | `10000` | gateway, order, ticket, mcp | |
| `GOOGLE_API_KEY` | — | **agent (wajib)**; catalog & ticket (opsional, tanpa ini → `keyword_fallback`) | Kunci Gemini |
| `AGENT_MODEL` | `gemini-flash-lite-latest` | agent | |
| `EMBEDDING_MODEL` | `text-embedding-004` | catalog, ticket | Harus configurable; lihat catatan risiko |
| `EMBEDDING_DIM` | `768` | catalog, ticket | Harus sama dengan dimensi respons model |
| `EMBEDDING_TIMEOUT_MS` | `5000` | catalog, ticket | |
| `SEMANTIC_MIN_SCORE` | `0.55` | catalog, ticket | Ambang cosine default (perlu dikalibrasi dengan data seed) |
| `NUXT_PUBLIC_API_BASE` | `http://localhost:8080` | frontend | |
| `APP_ENV` | `development` | semua | |

Aturan: fail-fast untuk variabel wajib; `INTERNAL_API_KEY`, `JWT_SECRET`, `GOOGLE_API_KEY` tidak boleh di-log; `.env` masuk `.gitignore`; perbandingan key `ConstantTimeCompare`.

---

## 9. Vector Search & RAG (Ringkasan Lintas Service)

| Aspek | `service-catalog` (produk) | `service-ticket` (kebijakan toko) |
|---|---|---|
| Unit yang diindeks | 1 vektor per produk | 1 vektor per **chunk** dokumen KB (≤ 800 karakter) |
| Teks embedding | `Nama / Kategori / Deskripsi / Spesifikasi` | `{judul}\n{chunk}` |
| Tabel vektor | `product_embeddings` | `knowledge_embeddings` (+ `knowledge_chunks`) |
| Endpoint | `GET /api/products/semantic-search?q=` | `GET /api/knowledge/search?q=` |
| MCP tool | `semantic_search_products` | `search_store_policy` |
| Hasil | Produk + `score`; harga/stok **selalu segar** dari tabel | Chunk + `title`, `category`, `score` |
| Trigger indexing | seed, `POST/PUT` produk, worker, `reindex` | seed, `POST/PUT` dokumen, worker, `reindex` |

Keputusan teknis bersama:
1. **Embedding:** Gemini Embedding API (default `text-embedding-004`, 768 dimensi); `taskType` `RETRIEVAL_DOCUMENT` saat indexing dan `RETRIEVAL_QUERY` saat query; vektor dinormalisasi L2.
2. **Penyimpanan vektor — `sqlite-vec`:** ekstensi C **tidak dapat dimuat `modernc.org/sqlite`**. Karena backend wajib pure-Go tanpa CGO, v1 menyimpan vektor sebagai `BLOB` dan menghitung cosine brute-force di Go (cukup untuk ≤ ±5.000 vektor). Akses disembunyikan di `vector.Store`; migrasi ke `sqlite-vec` (via `ncruces/go-sqlite3` WASM) atau **Qdrant lokal** dimungkinkan tanpa mengubah kontrak API. Itu di luar lingkup v1.
3. **Risiko model:** model embedding dapat dihentikan Google; `EMBEDDING_MODEL`/`EMBEDDING_DIM` configurable dan disimpan per baris (`model`, `dim`); ketidakcocokan memicu re-embed otomatis. Bila `text-embedding-004` tidak tersedia, gunakan `gemini-embedding-001`.
4. **Degradasi anggun:** tanpa `GOOGLE_API_KEY`/saat Embedding API gagal, service **tetap hidup**; endpoint pencarian beralih ke `LIKE` dengan `meta.mode="keyword_fallback"` dan `score: null`.
5. **Semantik vs eksak:** pencarian eksak (`GET /api/products`, `get_product`, `check_stock`) untuk ID, nama persis, dan filter terstruktur; pencarian semantik untuk kebutuhan/keluhan/ide bebas. Aturan pemilihan untuk agent ada di `06-SERVICE-AGENT.md` §4.
6. **Kebijakan toko tidak boleh dijawab dari ingatan model:** wajib melalui `search_store_policy`; hasil kosong = "tidak ada informasi".

---

## 10. Skenario End-to-End

### 10.1 Alur belanja via AI Agent (CUSTOMER `budi@toko.com`)
| # | Aktor | Aksi |
|---|---|---|
| 1 | Budi | RoleSwitcher → "Budi (CUSTOMER)" → `POST /auth/login` → JWT di `localStorage` |
| 2 | Budi | Chat: *"Cari powerbank di bawah 400 ribu."* (kriteria eksplisit) |
| 3 | Agent | Orchestrator → **Shopping Agent** → `search_products {query:"powerbank", max_price:400000}` → `GET /api/products?q=powerbank&max_price=400000` |
| 4 | Agent | Menjawab: Powerbank 20000mAh FastCharge (PRD-003) Rp299.000, stok 80 |
| 5 | Budi | *"Pesan 1 ya."* → **Order & Support Agent** meminta konfirmasi ringkasan |
| 6 | Budi | *"Ya, lanjut."* → `create_order` → `POST /api/orders` → order memanggil catalog `POST /internal/stock/decrement` → stok 80→79 |
| 7 | Agent | Menyebut ID order & batas bayar 15 menit; Budi membayar di UI → `PAID` |

### 10.2 Alur komplain via AI Agent (pesanan gagal)
| # | Aktor | Aksi |
|---|---|---|
| 1 | Budi | *"Pesanan ORD-003 kok belum jalan?"* → Order & Support Agent |
| 2 | Agent | `get_order ORD-003` → `PAYMENT_FAILED`, `failure_reason: "Saldo e-wallet tidak mencukupi"` |
| 3 | Agent | `search_store_policy "pembayaran gagal"` → KB-005; menjelaskan pesanan final, stok dikembalikan, opsi pesan ulang |
| 4 | Budi | *"Saldo saya sudah cukup tapi tetap gagal, buatkan tiket."* |
| 5 | Agent | `list_my_tickets(order_id=ORD-003)` → ada `TKT-001` aktif → memberi tahu nomor tiket, **tidak membuat duplikat** (jika belum ada: konfirmasi → `create_ticket`) |
| 6 | ADM-01 | `/admin/tickets` → `IN_PROGRESS` → `RESOLVED` |
| 7 | Budi | *"Status tiket saya?"* → `get_ticket` |

### 10.3 Alur pencarian semantik produk (RAG produk)
| # | Aktor | Aksi |
|---|---|---|
| 1 | Siti / GUEST | *"Aku butuh mouse yang tidak bikin pergelangan tangan pegal."* |
| 2 | Shopping Agent | Tidak ada ID/nama/filter eksak → `semantic_search_products {query: "..."}` |
| 3 | catalog | embed query → cosine → `PRD-014` (score tertinggi), `PRD-008` di bawahnya/ terbuang oleh `min_score` |
| 4 | Shopping Agent | `check_stock PRD-014` (verifikasi segar) → merekomendasikan **hanya** dari teks deskripsi/spesifikasi hasil tool |

### 10.4 Alur pertanyaan kebijakan sebelum membuat tiket (RAG kebijakan)
| # | Aktor | Aksi |
|---|---|---|
| 1 | Budi | *"Barang saya rusak, bisa dikembalikan nggak? Berapa lama?"* |
| 2 | Order & Support Agent | `search_store_policy {query:"syarat retur barang rusak"}` → `KB-001`, `KB-002` |
| 3 | Agent | Menjawab berdasarkan kutipan (retur ≤ 7 hari, wajib video unboxing) sambil menyebut judul dokumen sumber |
| 4 | Budi | *"Oke, saya mau ajukan."* → ringkasan tiket `PRODUCT` → konfirmasi → `create_ticket` |

### 10.5 Skenario negatif (wajib lulus uji)
| Skenario | Hasil |
|---|---|
| Panggil `service-order` tanpa `x-internal-api-key` | `401 INVALID_INTERNAL_KEY` |
| Budi `GET /api/orders/ORD-005` (milik Siti) | `404 NOT_FOUND` |
| GUEST tanya "status pesanan saya" ke agent | Agent minta login; tool order tidak tersedia/ditolak |
| Klien mengirim `x-user-role: ADMIN_SISTEM` | Dihapus Gateway |
| Tanya stok "PRD-999" | Tidak ditemukan; tidak mengarang |
| `/api/products/internal/...` dari browser | `404` di Gateway |
| Tanya kebijakan yang tidak ada di KB ("apakah ada cicilan 0%?") | `search_store_policy` kosong → agent: tidak ada informasi, tawarkan tiket; **tidak mengarang** |
| Embedding API mati | Pencarian tetap jalan dengan `mode: keyword_fallback`; agent menyampaikan hasil dengan hati-hati |

---

## 11. Urutan Implementasi yang Direkomendasikan
1. `internal/shared/*` (httputil, sqlitex, validate, svcclient, vector, embedding) + uji unit.
2. `service-catalog` (CRUD → stok → embedding & semantic-search) → 3. `service-order` → 4. `service-ticket` (tiket → KB & search) → 5. `service-gateway` → 6. `service-mcp` → 7. `service-agent` → 8. `service-frontend`.

Tiap service Go (termasuk mcp & agent): `main.go` + `GET /health` → middleware shared → migrate & seed → endpoint → uji `curl` → `CGO_ENABLED=0 go build`.

## 12. Daftar Dokumen
| File | Isi |
|---|---|
| `01-SERVICE-GATEWAY.md` | Go, auth.db, JWT, `httputil.ReverseProxy`, stripBrowserOrigin |
| `02-SERVICE-CATALOG.md` | Go, katalog, stok, embedding produk, `semantic-search` |
| `03-SERVICE-ORDER.md` | Go, pesanan, integrasi stok via HTTP client native |
| `04-SERVICE-TICKET.md` | Go, tiket, knowledge base kebijakan, `/api/knowledge/search` |
| `05-SERVICE-MCP.md` | 17 MCP Tools (Go) termasuk `semantic_search_products`, `search_store_policy` |
| `06-SERVICE-AGENT.md` | ADK, prompt, routing semantik vs eksak, anti-halusinasi |
| `07-SERVICE-FRONTEND.md` | Nuxt 3 SPA, composables, RoleSwitcher, ChatWidget |
