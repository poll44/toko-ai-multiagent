# 05 — PRD `service-mcp`

| Item | Nilai |
|---|---|
| Port | `5000` (`MCP_PORT`) |
| Stack | **Go** native `net/http` + `github.com/modelcontextprotocol/go-sdk` (v1.8) + `github.com/google/jsonschema-go` (JSON Schema untuk validasi input); satu module dengan service lain |
| Transport | **Utama:** Streamable HTTP **stateless** `POST /mcp` (GET/DELETE → 405 oleh SDK). **Dev:** Stdio (`go run ./service-mcp --stdio`). HTTP+SSE legacy tidak diimplementasikan (opsional) |
| Database | Tidak ada (stateless) |
| Peran | Membungkus REST API **service Go** `catalog` (:4001), `order` (:4002), `ticket` (:4003) menjadi **17 MCP Tools**; meneruskan identitas user |
| Pemanggil | `service-agent` (`MCP_URL=http://localhost:5000/mcp`) |
| Kontrak upstream | Path `/api/...`, JSON `snake_case`, `{"data":...}`/`{"error":...}` — Master §7. MCP ditulis dalam Go dan memakai ulang `internal/shared` (`httputil`, `svcclient`, `config`) |
| Rujukan | `00-MASTER-PRD.md` |

## 1. Prinsip
1. **MCP tidak memiliki logika bisnis & tidak punya akses DB.** Setiap tool = 1 panggilan REST (atau komposisi sederhana).
2. **Tidak ada otorisasi di MCP sebagai satu-satunya lapisan.** MCP hanya membatasi *tool yang terlihat* per role (UX/hemat token); penegakan sebenarnya tetap di service domain.
3. **Identitas selalu dari header transport MCP**, tidak pernah dari argumen tool (LLM tidak boleh mengisi `user_id`).
4. Tool mengembalikan **JSON apa adanya** dari REST (dalam `content[0].text` sebagai JSON string; `structuredContent` **tidak dipakai** agar body tidak perlu `outputSchema`). Error REST dikembalikan sebagai `isError: true` dengan isi error standar agar agent bisa menjelaskan.

## 2. Autentikasi & Forwarding Header

### 2.1 Header masuk (dari `service-agent`) — transport HTTP
| Header | Wajib | Perilaku MCP |
|---|:-:|---|
| `x-internal-api-key` | ✅ | Harus = `INTERNAL_API_KEY`, jika tidak `401 INVALID_INTERNAL_KEY` (HTTP-level, sebelum MCP handshake) |
| `x-user-id` | ✅ | Identitas. Format `^(USR|ADM|DEV)-\d{2,}$` atau `GUEST`; selain itu `400 VALIDATION_ERROR` |
| `x-user-role` | ✅ | ∈ `GUEST`/`CUSTOMER`/`ADMIN_SISTEM`/`ADMIN_DEV` |
| `x-request-id` | ❌ | Diteruskan; dibuat baru jika kosong |

Mode **stateless** (`StreamableHTTPOptions{Stateless: true}`): untuk SETIAP request HTTP, middleware (`InternalAuthMiddleware` → `IdentityMiddleware` → cek format `x-user-id`) memvalidasi header dan menyimpan `Identity` di context; `getServer(r)` membangun `mcp.Server` baru yang hanya memuat tool untuk role itu dan menangkap identitas lewat **closure**. Tidak ada sesi MCP yang dapat dibajak lintas user (tidak ada state bersama antar-request). Validasi gagal diputus sebelum handshake: key salah → `401 INVALID_INTERNAL_KEY`, role tidak valid → `403`, `x-user-id` tak valid → `400`.

### 2.2 Header keluar (ke `catalog`/`order`/`ticket`)
| Header | Nilai |
|---|---|
| `x-internal-api-key` | `INTERNAL_API_KEY` milik MCP |
| `x-user-id` | dari konteks request (header masuk) |
| `x-user-role` | dari konteks request |
| `x-request-id` | dari konteks |
| `x-service-name` | `service-mcp` |
| `Content-Type` | `application/json` |

> `/internal/*` pada catalog hanya menerima `x-service-name: service-order`, sehingga MCP **tidak dapat** memanggil endpoint stok internal — memang disengaja.

### 2.3 Mode Stdio (dev saja)
Tidak ada header per-request. Identitas dari env: `MCP_DEV_USER_ID` (default `GUEST`), `MCP_DEV_USER_ROLE` (default `GUEST`). Dilarang aktif bila `APP_ENV=production`.

### 2.4 Pembatasan tool per role (*tool visibility*)
`tools/list` difilter berdasarkan `x-user-role`; `tools/call` pada tool yang tak diizinkan → error MCP `isError` dengan `FORBIDDEN`.

| Tool | GUEST | CUSTOMER | ADMIN_SISTEM | ADMIN_DEV |
|---|:-:|:-:|:-:|:-:|
| `search_products`, `semantic_search_products`, `get_product`, `list_categories`, `check_stock` | ✅ | ✅ | ✅ | ✅ |
| `search_store_policy` | ✅ | ✅ | ✅ | ✅ |
| `create_order`, `pay_order`, `cancel_order` | ❌ | ✅ | ❌ | ❌ |
| `list_my_orders`, `get_order` | ❌ | ✅ | ✅ (semua) | ✅ (semua) |
| `create_ticket` | ❌ | ✅ | ❌ | ❌ |
| `list_my_tickets`, `get_ticket`, `add_ticket_message` | ❌ | ✅ | ✅ | ✅ |
| `update_ticket_status` | ❌ | ✅ (reopen/close) | ✅ | ✅ (TECHNICAL) |
| `adjust_product_stock` | ❌ | ❌ | ✅ | ❌ |

## 3. Daftar MCP Tools

Konvensi: nama `snake_case`; semua input memakai JSON Schema `additionalProperties:false` (setara Zod `.strict()`; argumen tak dikenal seperti `user_id` DITOLAK sebelum handler); input **tidak pernah** memuat `user_id`/`role`. Respons sukses = **body REST apa adanya**: objek tunggal → `{"data":{...}}`; daftar → `{"data":[...],"meta":{...}}` (`meta` wajib diteruskan karena memuat `mode`/`score` pencarian semantik). HTTP error REST dipetakan: `status >= 400` → `isError: true` dengan JSON `{"error":{"code","message","details","request_id"}}` asli dari service Go.

### 3.1 Catalog

#### `search_products` (pencarian **eksak** / terstruktur)
- **Deskripsi:** "Pencarian EKSAK produk: kata kunci literal (nama/merek), kategori, rentang harga, urutan. Gunakan bila pengguna menyebut nama produk, kata kunci jelas, atau filter harga/kategori. Untuk kebutuhan/keluhan/deskripsi bebas gunakan semantic_search_products."
- **Input (ditulis dalam notasi Zod sebagai ringkasan; implementasi = JSON Schema strict di `internal/tools`):**
```ts
z.object({
  query: z.string().max(100).optional(),
  category: z.enum(['audio','aksesoris','wearable','komputer','penyimpanan']).optional(),
  min_price: z.number().int().min(0).optional(),
  max_price: z.number().int().min(0).optional(),
  in_stock_only: z.boolean().optional(),
  sort: z.enum(['name','price_asc','price_desc','newest']).optional(),
  limit: z.number().int().min(1).max(20).default(10),
}).strict()
```
- **Mapping:** `GET {CATALOG_URL}/api/products?q=&category=&min_price=&max_price=&in_stock=&sort=&limit=` (`query→q`, `in_stock_only→in_stock`).

#### `semantic_search_products` (pencarian **semantik**, vektor)
- **Deskripsi:** "Pencarian SEMANTIK produk berdasarkan makna kebutuhan, keluhan, atau skenario pemakaian dalam bahasa natural (mis. 'mouse yang tidak bikin pergelangan pegal', 'headphone untuk di kereta yang berisik'). Hasil diurutkan berdasarkan kemiripan (score 0–1). Gunakan bila pengguna TIDAK menyebut nama/ID/filter eksak. Setelah itu verifikasi stok/harga dengan check_stock atau get_product sebelum menawarkan."
- **Input (ditulis dalam notasi Zod sebagai ringkasan; implementasi = JSON Schema strict di `internal/tools`):**
```ts
z.object({
  query: z.string().trim().min(3).max(300),          // kalimat kebutuhan/keluhan apa adanya
  category: z.enum(['audio','aksesoris','wearable','komputer','penyimpanan']).optional(),
  max_price: z.number().int().min(0).optional(),
  min_price: z.number().int().min(0).optional(),
  in_stock_only: z.boolean().optional(),
  limit: z.number().int().min(1).max(10).default(5),
}).strict()
```
- **Mapping:** `GET {CATALOG_URL}/api/products/semantic-search?q=&category=&max_price=&min_price=&in_stock=&limit=` (`query→q`, `in_stock_only→in_stock`). `min_score` **tidak** diekspos ke LLM (memakai default server).
- **Output (isi `content[0].text`):** body REST apa adanya:
```json
{
  "data": [ { "id": "PRD-014", "name": "Mouse Vertikal Ergonomis ErgoGlide V2", "description": "...", "specifications": "...",
              "category": "komputer", "price": 289000, "stock": 35, "stock_status": "IN_STOCK",
              "image_url": null, "is_active": true, "created_at": "...", "updated_at": "...", "score": 0.8123 } ],
  "meta": { "total": 1, "limit": 5, "query": "mouse yang tidak bikin pergelangan pegal", "mode": "semantic", "min_score": 0.55 }
}
```
  `meta.mode = "keyword_fallback"` berarti embedding tak tersedia (`score: null`); agent harus menyampaikan ketidakpastian. `data: []` = tidak ada produk yang cukup relevan.

#### `get_product`
- **Deskripsi:** "Ambil detail satu produk berdasarkan ID (mis. PRD-003)."
- **Input:** `z.object({ product_id: z.string().regex(/^PRD-\d{3,}$/) }).strict()`
- **Mapping:** `GET /api/products/:product_id`

#### `list_categories`
- **Deskripsi:** "Daftar kategori produk beserta jumlah produknya."
- **Input:** `z.object({}).strict()`
- **Mapping:** `GET /api/products/categories`

#### `check_stock`
- **Deskripsi:** "Cek ketersediaan stok satu produk (stok, stock_status)."
- **Input:** `z.object({ product_id: z.string().regex(/^PRD-\d{3,}$/) }).strict()`
- **Mapping:** `GET /api/products/:product_id` → kembalikan hanya `{id, name, stock, stock_status, price}`.

#### `adjust_product_stock` (ADMIN_SISTEM)
- **Input:** `z.object({ product_id: z.string().regex(/^PRD-\d{3,}$/), mode: z.enum(['set','adjust']), value: z.number().int(), note: z.string().max(200).optional() }).strict()`
- **Mapping:** `PATCH /api/products/:product_id/stock` body `{mode,value,note}`.

### 3.1b Knowledge Base (service-ticket)

#### `search_store_policy`
- **Deskripsi:** "Cari kebijakan/FAQ/SOP resmi toko (retur, garansi, estimasi pengiriman, pembayaran gagal/kedaluwarsa, pembatalan, refund, prosedur komplain, jam layanan) secara semantik. WAJIB dipanggil sebelum menjawab pertanyaan kebijakan dan sebelum memutuskan membuat tiket untuk hal yang mungkin sudah diatur kebijakan. Jawab hanya berdasarkan hasil; jika kosong, katakan tidak ada informasi."
- **Input (ditulis dalam notasi Zod sebagai ringkasan; implementasi = JSON Schema strict di `internal/tools`):**
```ts
z.object({
  query: z.string().trim().min(3).max(300),
  category: z.enum(['RETURN','WARRANTY','SHIPPING','PAYMENT','ORDER','COMPLAINT','GENERAL']).optional(),
  limit: z.number().int().min(1).max(5).default(3),
}).strict()
```
- **Mapping:** `GET {TICKET_URL}/api/knowledge/search?q=&category=&limit=` (`query→q`).
- **Output:** body REST apa adanya:
```json
{
  "data": [ { "chunk_id": "KB-005#1", "document_id": "KB-005", "title": "Pembayaran Gagal dan Kedaluwarsa",
              "category": "PAYMENT", "content": "Batas waktu pembayaran adalah 15 menit ...", "score": 0.8431 } ],
  "meta": { "total": 1, "limit": 3, "query": "...", "mode": "semantic", "min_score": 0.55 }
}
```
  `data: []` ⇒ kebijakan tidak ditemukan (bukan berarti "boleh"/"tidak boleh"). Dapat dipanggil GUEST (informasi publik).

### 3.2 Order

#### `create_order` (CUSTOMER)
- **Deskripsi:** "Buat pesanan baru. HANYA panggil setelah pelanggan secara eksplisit mengonfirmasi produk, jumlah, metode pembayaran, dan alamat."
- **Input:**
```ts
z.object({
  items: z.array(z.object({
    product_id: z.string().regex(/^PRD-\d{3,}$/),
    quantity: z.number().int().min(1).max(99),
  }).strict()).min(1).max(20),
  payment_method: z.enum(['EWALLET','BANK_TRANSFER','CREDIT_CARD']),
  shipping_address: z.string().min(10).max(300),
  confirmed_by_user: z.literal(true),   // guard: agent wajib set true setelah konfirmasi
}).strict()
```
- **Mapping:** `POST {ORDER_URL}/api/orders` body tanpa `confirmed_by_user`.

#### `list_my_orders`
- **Deskripsi:** "Daftar pesanan milik user yang sedang login (admin: semua)."
- **Input:** `z.object({ status: z.enum(['PENDING_PAYMENT','PAID','PROCESSING','SHIPPED','DELIVERED','CANCELLED','PAYMENT_FAILED','PAYMENT_TIMEOUT']).optional(), limit: z.number().int().min(1).max(20).default(10) }).strict()`
- **Mapping:** `GET /api/orders?status=&limit=`

#### `get_order`
- **Deskripsi:** "Detail pesanan: status, status pembayaran, alasan gagal (failure_reason), resi, item."
- **Input:** `z.object({ order_id: z.string().regex(/^ORD-\d{3,}$/) }).strict()`
- **Mapping:** `GET /api/orders/:order_id`

#### `pay_order` (CUSTOMER)
- **Deskripsi:** "Bayar ulang/proses pembayaran (simulasi) untuk pesanan PENDING_PAYMENT milik user. Butuh konfirmasi eksplisit."
- **Input:** `z.object({ order_id: z.string().regex(/^ORD-\d{3,}$/), confirmed_by_user: z.literal(true) }).strict()`
- **Mapping:** `POST /api/orders/:order_id/pay` body `{simulate:"success"}`.
> Catatan: tool ini selalu mensimulasikan sukses; skenario `fail`/`timeout` hanya lewat UI/QA.

#### `cancel_order` (CUSTOMER)
- **Input:** `z.object({ order_id: z.string().regex(/^ORD-\d{3,}$/), reason: z.string().max(200).optional(), confirmed_by_user: z.literal(true) }).strict()`
- **Mapping:** `POST /api/orders/:order_id/cancel` body `{reason}`.

### 3.3 Ticket

#### `create_ticket` (CUSTOMER)
- **Deskripsi:** "Buat tiket komplain/bantuan. Panggil setelah pelanggan mengonfirmasi ringkasan tiket."
- **Input:**
```ts
z.object({
  subject: z.string().min(5).max(120),
  description: z.string().min(10).max(2000),
  category: z.enum(['PAYMENT','SHIPPING','PRODUCT','TECHNICAL','OTHER']),
  priority: z.enum(['LOW','MEDIUM','HIGH']).default('MEDIUM'),
  order_id: z.string().regex(/^ORD-\d{3,}$/).optional(),
  confirmed_by_user: z.literal(true),
}).strict()
```
- **Mapping:** `POST {TICKET_URL}/api/tickets`.

#### `list_my_tickets`
- **Input:** `z.object({ status: z.enum(['OPEN','IN_PROGRESS','RESOLVED','CLOSED']).optional(), order_id: z.string().regex(/^ORD-\d{3,}$/).optional(), limit: z.number().int().min(1).max(20).default(10) }).strict()`
- **Mapping:** `GET /api/tickets?status=&order_id=&limit=`

#### `get_ticket`
- **Input:** `z.object({ ticket_id: z.string().regex(/^TKT-\d{3,}$/) }).strict()`
- **Mapping:** `GET /api/tickets/:ticket_id`

#### `add_ticket_message`
- **Input:** `z.object({ ticket_id: z.string().regex(/^TKT-\d{3,}$/), message: z.string().min(1).max(2000) }).strict()`
- **Mapping:** `POST /api/tickets/:ticket_id/messages`

#### `update_ticket_status`
- **Deskripsi:** "Ubah status tiket sesuai lifecycle (customer: reopen/close; admin: proses & selesaikan)."
- **Input:** `z.object({ ticket_id: z.string().regex(/^TKT-\d{3,}$/), status: z.enum(['OPEN','IN_PROGRESS','RESOLVED','CLOSED']), resolution_note: z.string().min(10).max(500).optional(), message: z.string().max(2000).optional() }).strict()`
- **Mapping:** `PATCH /api/tickets/:ticket_id/status`

## 4. Ringkasan Mapping Endpoint
| Tool | Method | Service | Path |
|---|---|---|---|
| `search_products` | GET | catalog | `/api/products` |
| `semantic_search_products` | GET | catalog | `/api/products/semantic-search` |
| `search_store_policy` | GET | ticket | `/api/knowledge/search` |
| `get_product`, `check_stock` | GET | catalog | `/api/products/:id` |
| `list_categories` | GET | catalog | `/api/products/categories` |
| `adjust_product_stock` | PATCH | catalog | `/api/products/:id/stock` |
| `create_order` | POST | order | `/api/orders` |
| `list_my_orders` | GET | order | `/api/orders` |
| `get_order` | GET | order | `/api/orders/:id` |
| `pay_order` | POST | order | `/api/orders/:id/pay` |
| `cancel_order` | POST | order | `/api/orders/:id/cancel` |
| `create_ticket` | POST | ticket | `/api/tickets` |
| `list_my_tickets` | GET | ticket | `/api/tickets` |
| `get_ticket` | GET | ticket | `/api/tickets/:id` |
| `add_ticket_message` | POST | ticket | `/api/tickets/:id/messages` |
| `update_ticket_status` | PATCH | ticket | `/api/tickets/:id/status` |

## 5. Perilaku HTTP Client Internal
- `svcclient.DoRaw` (`net/http`, timeout `UPSTREAM_TIMEOUT_MS`); tanpa retry. Body upstream diteruskan APA ADANYA.
- Jaringan/timeout → tool `isError` dengan body error standar (`UPSTREAM_ERROR` 502 / `UPSTREAM_TIMEOUT` 504, `details:null`, `request_id`).
- Log terstruktur (`slog`): request id, user id, path, status, ms — **jangan** log `INTERNAL_API_KEY`/isi pesan sensitif.
- Batas ukuran respons upstream 4 MB; jumlah item dibatasi oleh `limit` pada schema (maks 20).

## 6. Struktur Kode
```
service-mcp/
├── main.go                    # flag --stdio; HTTP: /health + /mcp (middleware auth→identity→strictUserID)
├── main_test.go               # klien MCP sungguhan + upstream palsu (auth, visibilitas per role, header, validasi, paralel)
└── internal/
    ├── config/config.go
    └── tools/{schema.go,tools.go}   # 17 definisi tool (JSON Schema strict + Run → svcclient.DoRaw) + NewServer(identity)
```

## 7. Kriteria Penerimaan
- [ ] Tanpa `x-internal-api-key` → `401` sebelum handshake.
- [ ] Total 17 tool terdaftar; `tools/list` GUEST = 6 (5 katalog termasuk `semantic_search_products` + `search_store_policy`), CUSTOMER = 16 (tanpa `adjust_product_stock`), ADMIN_SISTEM = 13, ADMIN_DEV = 12.
- [ ] `semantic_search_products {query:"mouse yang tidak bikin pergelangan pegal"}` → `data[0].id = "PRD-014"` dan `meta` (`mode`, `score`) ikut diteruskan.
- [ ] `search_store_policy {query:"berapa lama batas bayar"}` → memuat `KB-005`; query tak relevan → `data: []` (bukan error).
- [ ] Tool pencarian semantik dengan `query` < 3 karakter → ditolak validasi schema; `502/503` upstream → `isError` dengan `code` asli.
- [ ] `get_order {order_id:"ORD-005"}` oleh `USR-01` → `isError` `NOT_FOUND` (header identitas benar diteruskan).
- [ ] Argumen `user_id` ekstra → ditolak schema (`additionalProperties:false`) dan TIDAK sampai ke upstream.
- [ ] `create_order` tanpa `confirmed_by_user: true` → ditolak validasi.
- [ ] Dua request bersamaan dari user berbeda tidak saling menimpa konteks identitas.
- [ ] Request paralel dari 4 identitas berbeda: setiap request upstream memuat identitas pemiliknya sendiri (tidak bocor antar-request).
