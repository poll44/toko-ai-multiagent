# 03 — TRD `service-order`

| Item | Nilai |
|---|---|
| Port | `4002` |
| Stack | **Go ≥ 1.22 native `net/http`**, `database/sql` + `modernc.org/sqlite` (pure-Go) |
| Database | `order.db` (`ORDER_DB_PATH`) |
| Peran | Pesanan, simulasi pembayaran, orkestrasi stok dengan `service-catalog` |
| Dependensi | `service-catalog` (`CATALOG_URL`) via `net/http` client |
| Rujukan | `00-MASTER-TRD.md` |

## 1. Struktur Kode
```
service-order/
├── main.go                       # config, DB, seed, expirer goroutine, mux, graceful shutdown
└── internal/
    ├── config/config.go
    ├── store/{db.go,migrate.go,seed.go,order_repo.go}
    ├── catalogclient/client.go   # Decrement(), Restock() — HTTP client native
    ├── service/order_service.go  # state machine + kompensasi
    ├── jobs/expire_payments.go
    └── handlers/{orders.go,health.go}
```
Memakai `internal/shared/*` (`httputil`, `sqlitex`, `validate`, `svcclient`).

## 2. Routing & Middleware
```go
secured := httputil.Chain(httputil.InternalAuthMiddleware(key), httputil.IdentityMiddleware(), denyGuest)
mux.Handle("POST /api/orders",              secured(requireRole("CUSTOMER")(create)))
mux.Handle("GET /api/orders",               secured(list))
mux.Handle("GET /api/orders/{id}",          secured(get))
mux.Handle("POST /api/orders/{id}/pay",     secured(requireRole("CUSTOMER")(pay)))
mux.Handle("POST /api/orders/{id}/cancel",  secured(requireRole("CUSTOMER","ADMIN_SISTEM")(cancel)))
mux.Handle("PATCH /api/orders/{id}/status", secured(requireRole("ADMIN_SISTEM")(setStatus)))
mux.HandleFunc("GET /health", health)
```
Urutan global: `Recover → RequestID → Logging → [/health] → InternalAuth → Identity → RequireRole`. `denyGuest`: Gateway sudah menolak GUEST (`401`); bila `x-user-role: GUEST` tetap sampai (mis. via MCP) → `403 FORBIDDEN`.

## 3. Skema SQLite `order.db`

### Tabel `orders`
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `id` | TEXT | PRIMARY KEY | `ORD-001`, … |
| `user_id` | TEXT | NOT NULL | pemilik |
| `status` | TEXT | NOT NULL CHECK(status IN ('PENDING_PAYMENT','PAID','PROCESSING','SHIPPED','DELIVERED','CANCELLED','PAYMENT_FAILED','PAYMENT_TIMEOUT')) | |
| `payment_status` | TEXT | NOT NULL CHECK(payment_status IN ('UNPAID','PAID','FAILED','EXPIRED','REFUNDED')) | |
| `payment_method` | TEXT | NOT NULL CHECK(payment_method IN ('EWALLET','BANK_TRANSFER','CREDIT_CARD')) | |
| `total_amount` | INTEGER | NOT NULL | Σ `subtotal` |
| `shipping_address` | TEXT | NOT NULL | |
| `tracking_number` | TEXT | NULL | |
| `failure_reason` | TEXT | NULL | |
| `payment_expires_at` | TEXT | NULL | RFC 3339 UTC |
| `paid_at` | TEXT | NULL | |
| `created_at` | TEXT | NOT NULL | |
| `updated_at` | TEXT | NOT NULL | |

Index: `idx_orders_user(user_id, created_at DESC)`, `idx_orders_status(status)`.

### Tabel `order_items`
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `order_id` | TEXT | NOT NULL REFERENCES orders(id) ON DELETE CASCADE | |
| `product_id` | TEXT | NOT NULL | |
| `product_name` | TEXT | NOT NULL | snapshot |
| `unit_price` | INTEGER | NOT NULL | snapshot |
| `quantity` | INTEGER | NOT NULL CHECK(quantity > 0) | |
| `subtotal` | INTEGER | NOT NULL | |

### Tabel `order_status_history`
| Kolom | Tipe | Constraint |
|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT |
| `order_id` | TEXT | NOT NULL REFERENCES orders(id) ON DELETE CASCADE |
| `from_status` | TEXT | NULL |
| `to_status` | TEXT | NOT NULL |
| `note` | TEXT | NULL |
| `actor_id` | TEXT | NOT NULL (`x-user-id` atau `SYSTEM`) |
| `created_at` | TEXT | NOT NULL |

DSN & pool sama seperti catalog (`_txlock=immediate`, `SetMaxOpenConns(1)`).

## 4. Mesin Status
```
PENDING_PAYMENT ──pay(success)──► PAID ──► PROCESSING ──► SHIPPED ──► DELIVERED
      │  ├──pay(fail)────────► PAYMENT_FAILED   (stok dikembalikan)
      │  ├──timeout (job)────► PAYMENT_TIMEOUT  (stok dikembalikan)
      │  └──cancel───────────► CANCELLED        (stok dikembalikan)
PAID/PROCESSING ──cancel(admin)──► CANCELLED    (payment_status=REFUNDED, stok dikembalikan)
```
| Dari | Ke | Pelaku |
|---|---|---|
| `PENDING_PAYMENT` | `PAID` | CUSTOMER pemilik via `/pay` (`simulate=success`) |
| `PENDING_PAYMENT` | `PAYMENT_FAILED` | `/pay` (`simulate=fail`) |
| `PENDING_PAYMENT` | `PAYMENT_TIMEOUT` | job sistem / `/pay` (`simulate=timeout`) |
| `PENDING_PAYMENT` | `CANCELLED` | CUSTOMER pemilik, ADMIN_SISTEM |
| `PAID` | `PROCESSING` | ADMIN_SISTEM |
| `PROCESSING` | `SHIPPED` | ADMIN_SISTEM (wajib `tracking_number`) |
| `SHIPPED` | `DELIVERED` | ADMIN_SISTEM |
| `PAID`,`PROCESSING` | `CANCELLED` | ADMIN_SISTEM saja |

Transisi lain → `409 INVALID_STATE_TRANSITION`. Status akhir: `PAYMENT_FAILED`, `PAYMENT_TIMEOUT`, `CANCELLED`, `DELIVERED`. Transisi diimplementasikan sebagai `map[Status][]Status` + pemeriksaan pelaku di `order_service.go`.

## 5. Data Seed (hanya bila `orders` kosong)
Timestamp relatif saat seed (`now − N hari`). Harga mengikuti katalog seed.

| `id` | `user_id` | `status` | `payment_status` | Item | `total_amount` | Catatan uji |
|---|---|---|---|---|---:|---|
| `ORD-001` | `USR-01` | `DELIVERED` | `PAID` | PRD-001×1, PRD-004×2 | 527000 | riwayat normal |
| `ORD-002` | `USR-01` | `SHIPPED` | `PAID` | PRD-006×1 | 899000 | `tracking_number=JNE-TRK-0002` |
| `ORD-003` | `USR-01` | `PAYMENT_FAILED` | `FAILED` | PRD-007×1 | 649000 | `failure_reason="Saldo e-wallet tidak mencukupi"`, `EWALLET` — **skenario komplain utama** |
| `ORD-004` | `USR-01` | `PAYMENT_TIMEOUT` | `EXPIRED` | PRD-012×1 | 1299000 | `failure_reason="Batas waktu pembayaran terlewati (15 menit)"` — **skenario timeout** |
| `ORD-005` | `USR-02` | `PROCESSING` | `PAID` | PRD-003×2, PRD-005×1 | 847000 | milik Siti (uji isolasi) |
| `ORD-006` | `USR-02` | `PENDING_PAYMENT` | `UNPAID` | PRD-002×1 | 799000 | `payment_expires_at = now + 10 menit` |
| `ORD-007` | `USR-02` | `CANCELLED` | `UNPAID` | PRD-008×1 | 159000 | dibatalkan user |
| `ORD-008` | `USR-02` | `DELIVERED` | `PAID` | PRD-009×1, PRD-011×1 | 408000 | riwayat normal |

Seed menulis `order_status_history` konsisten; **tidak** memanggil katalog. `shipping_address`: "Jl. Merdeka No. 10, Jakarta" (USR-01), "Jl. Melati No. 5, Bandung" (USR-02).

## 6. Kontrak REST API

Konvensi: Master §7. Representasi `Order`:
```json
{
  "id": "ORD-003", "user_id": "USR-01", "status": "PAYMENT_FAILED",
  "payment_status": "FAILED", "payment_method": "EWALLET", "total_amount": 649000,
  "shipping_address": "Jl. Merdeka No. 10, Jakarta", "tracking_number": null,
  "failure_reason": "Saldo e-wallet tidak mencukupi", "payment_expires_at": null, "paid_at": null,
  "items": [ { "product_id": "PRD-007", "product_name": "Keyboard Mekanikal K87 RGB", "unit_price": 649000, "quantity": 1, "subtotal": 649000 } ],
  "created_at": "...", "updated_at": "..."
}
```
`GET /api/orders/{id}` menambah `history: [{from_status,to_status,note,actor_id,created_at}]`. Field nullable tampil sebagai `null` (struct Go memakai pointer tanpa `omitempty`).

| Method | Path | Role | Deskripsi |
|---|---|---|---|
| POST | `/api/orders` | CUSTOMER | Buat order |
| GET | `/api/orders` | CUSTOMER (milik sendiri), ADMIN_SISTEM, ADMIN_DEV (semua) | Daftar |
| GET | `/api/orders/{id}` | sama | Detail |
| POST | `/api/orders/{id}/pay` | CUSTOMER pemilik | Simulasi pembayaran |
| POST | `/api/orders/{id}/cancel` | CUSTOMER pemilik (`PENDING_PAYMENT`), ADMIN_SISTEM | Batalkan |
| PATCH | `/api/orders/{id}/status` | ADMIN_SISTEM | Fulfilment |
| GET | `/health` | publik | `{"data":{"status":"ok","service":"service-order"}}` |

### 6.1 `POST /api/orders` (CUSTOMER)
```json
{
  "items": [ { "product_id": "PRD-003", "quantity": 1 } ],
  "payment_method": "EWALLET",
  "shipping_address": "Jl. Merdeka No. 10, Jakarta"
}
```
| Field | Validasi |
|---|---|
| `items` | 1–20 item, `product_id` unik (`^PRD-\d{3,}$`), `quantity` 1–99. Field harga di body **diabaikan** (`ReadJSON` tidak memakai `DisallowUnknownFields` untuk endpoint ini) |
| `payment_method` | enum |
| `shipping_address` | 10–300 karakter |

`201 { "data": Order }` — `status=PENDING_PAYMENT`, `payment_expires_at = now + ORDER_PAYMENT_TTL_MINUTES`. `user_id` selalu dari `x-user-id`.

**Alur internal `service-order` → `service-catalog` (validasi & pengurangan stok):**
```
1. httputil.ReadJSON + validate.
2. Generate id (ORD-xxx) dalam transaksi pembuat nomor (SELECT MAX).
3. catalogclient.Decrement(ctx, ids, reference_id="ORD-xxx", items)
     POST {CATALOG_URL}/internal/stock/decrement
     headers: x-internal-api-key, x-service-name: service-order,
              x-user-id / x-user-role / x-request-id (diteruskan dari request asal)
   ├─ 409 INSUFFICIENT_STOCK → teruskan 409 + details ke klien
   ├─ 404 NOT_FOUND / 422 PRODUCT_INACTIVE → teruskan
   ├─ timeout/jaringan → 504/502 UPSTREAM_*  (stok tak berubah; order tidak dibuat)
   └─ 200 → unit_price & name sebagai snapshot
4. Transaksi order.db: INSERT orders + order_items + order_status_history.
   └─ Gagal → KOMPENSASI catalogclient.Restock(reason "ORDER_CREATE_FAILED") lalu 500.
5. 201.
```

### 6.2 HTTP Client Native Go (`catalogclient` + `shared/svcclient`)
```go
type Client struct{ base string; key string; hc *http.Client }   // hc = &http.Client{Timeout: UPSTREAM_TIMEOUT_MS}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
    // json.Marshal(in) → http.NewRequestWithContext(ctx, method, c.base+path, body)
    // header: Content-Type, x-internal-api-key, x-service-name:"service-order",
    //         x-user-id, x-user-role, x-request-id ← httputil.IdentityFrom(ctx)
    // resp: status ≥ 400 → decode {"error":{...}} → *svcclient.UpstreamError{Status, Code, Message, Details}
    // jaringan/timeout → *svcclient.UpstreamError{Status:502|504, Code:"UPSTREAM_ERROR"|"UPSTREAM_TIMEOUT"}
}
```
| Aturan | Nilai |
|---|---|
| `Decrement` | **Tanpa retry** (hindari potong stok ganda) |
| `Restock` | Retry maks 2× (backoff 200 ms) — idempoten di catalog |
| Handler | `errors.As(err, &upstreamErr)` → `httputil.WriteError(w, upstreamErr.Status, upstreamErr.Code, upstreamErr.Message, upstreamErr.Details)` |
| Konteks | Gunakan `r.Context()` agar pembatalan klien memutus panggilan |

### 6.3 `GET /api/orders`
| Query | Keterangan |
|---|---|
| `status` | filter |
| `user_id` | hanya ADMIN_*; CUSTOMER diabaikan (paksa `x-user-id`) |
| `page`, `limit` | default 1, 20 (maks 100) |

Urut `created_at DESC`; `meta: {total,page,limit}`.

### 6.4 `GET /api/orders/{id}`
CUSTOMER bukan pemilik → `404 NOT_FOUND`. ADMIN_* semua.

### 6.5 `POST /api/orders/{id}/pay` (CUSTOMER pemilik)
`{ "simulate": "success" | "fail" | "timeout", "fail_reason": "opsional" }` (default `success`).
| Prasyarat | Error |
|---|---|
| status `PENDING_PAYMENT` | `409 INVALID_STATE_TRANSITION` |
| `payment_expires_at` belum lewat (jika lewat → otomatis `PAYMENT_TIMEOUT` + restock, lalu `409`) | |

| `simulate` | Efek |
|---|---|
| `success` | `PAID`, `payment_status=PAID`, `paid_at=now`, `payment_expires_at=null` |
| `fail` | `PAYMENT_FAILED`, `FAILED`, `failure_reason = fail_reason ?? "Pembayaran ditolak oleh penyedia pembayaran"`, **restock** |
| `timeout` | `PAYMENT_TIMEOUT`, `EXPIRED`, `failure_reason="Batas waktu pembayaran terlewati (15 menit)"`, **restock** |

`200 { "data": Order }`.

### 6.6 `POST /api/orders/{id}/cancel`
Body opsional `{ "reason": "..." }`. CUSTOMER: hanya `PENDING_PAYMENT` miliknya. ADMIN_SISTEM: `PENDING_PAYMENT`/`PAID`/`PROCESSING` (`REFUNDED` bila sebelumnya `PAID`). Restock. `200 { "data": Order }`.

### 6.7 `PATCH /api/orders/{id}/status` (ADMIN_SISTEM)
`{ "status": "SHIPPED", "tracking_number": "JNE-TRK-0099", "note": "..." }`. `tracking_number` wajib untuk `SHIPPED` (5–40 char). Hanya transisi fulfilment §4.

## 7. Job Kedaluwarsa Pembayaran
Goroutine `jobs.ExpirePayments(ctx, svc, 60*time.Second)` dengan `time.Ticker`; berhenti saat `ctx` dibatalkan (graceful shutdown). Setiap tick: order `PENDING_PAYMENT` dengan `payment_expires_at < now` → `PAYMENT_TIMEOUT`/`EXPIRED`, `failure_reason` standar, `Restock` (konteks job tanpa identitas user: `x-user-id=SYSTEM`, `x-user-role=ADMIN_SISTEM`, `x-service-name=service-order`), history `actor_id='SYSTEM'`. Dijalankan sekali saat start. Idempoten.

## 8. Dipanggil Service Lain
`service-ticket` memanggil `GET {ORDER_URL}/api/orders/{id}` dengan identitas user asli (`x-user-id`, `x-user-role`), `x-internal-api-key`, `x-service-name: service-ticket`. Non-pemilik → `404`.

## 9. Kriteria Penerimaan
- [ ] `CGO_ENABLED=0 go build ./service-order` sukses.
- [ ] Order sukses mengurangi stok katalog tepat sebesar quantity (cek `stock_movements`).
- [ ] Qty > stok → `409 INSUFFICIENT_STOCK` dan tidak ada baris order.
- [ ] Katalog mati saat create → `502 UPSTREAM_ERROR`, tidak ada order; katalog lambat → `504 UPSTREAM_TIMEOUT`.
- [ ] Siti `GET /api/orders/ORD-003` → `404`; Budi `GET /api/orders` → hanya ORD-001..004.
- [ ] `ADMIN_DEV` bisa baca semua tetapi `POST /api/orders/{id}/pay` → `403`.
- [ ] `pay simulate=fail` → `PAYMENT_FAILED` + stok kembali.
- [ ] `ORD-006` kedaluwarsa otomatis setelah 10 menit → `PAYMENT_TIMEOUT`, stok PRD-002 +1.
- [ ] Harga di body klien diabaikan; total dari katalog.
- [ ] `SIGTERM` menghentikan server dan job tanpa panic.
