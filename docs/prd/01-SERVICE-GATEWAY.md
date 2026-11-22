# 01 — PRD `service-gateway`

| Item | Nilai |
|---|---|
| Port | `8080` |
| Stack | **Go ≥ 1.22 native `net/http`** (`http.ServeMux` pola `METHOD /path/{param}`), tanpa web framework |
| Library eksternal | `github.com/golang-jwt/jwt/v5`, `modernc.org/sqlite` (pure-Go, tanpa CGO) |
| Database | `auth.db` (`AUTH_DB_PATH`) via `database/sql` |
| Peran | Titik masuk tunggal: autentikasi, verifikasi JWT, injeksi header identitas, reverse proxy |
| Rujukan | `00-MASTER-PRD.md` (konvensi JSON, header, RBAC, error, shared package) |

## 1. Tanggung Jawab
1. Menyimpan user demo & memverifikasi kredensial (`/auth/login`).
2. Menerbitkan & memverifikasi **JWT stateless** (HS256). Satu-satunya service yang memegang `JWT_SECRET`.
3. Menyuntikkan `x-user-id`, `x-user-role`, `x-internal-api-key`, `x-request-id` ke setiap request proxy.
4. Mem-proxy ke `catalog`, `order`, `ticket`, `agent` memakai `httputil.NewSingleHostReverseProxy`.
5. CORS untuk origin frontend; `stripBrowserOrigin` khusus `/api/agent`.

## 2. Struktur Kode (di dalam monorepo Go — lihat Master §4)
```
service-gateway/
├── main.go                       # load config, open DB, build mux, http.Server, graceful shutdown
└── internal/
    ├── config/config.go          # env → struct (fail-fast)
    ├── store/{db.go,users.go,seed.go}   # database/sql + modernc.org/sqlite
    ├── auth/{jwt.go,handlers.go}        # Sign/Parse, /auth/*
    ├── middleware/{cors.go,sanitize.go,blockinternal.go,authenticate.go,requirerole.go}
    ├── proxy/{proxy.go,agent_identity.go}
    └── handlers/dev_health.go
```
Memakai shared package: `internal/shared/httputil` (`WriteJSON`, `WriteError`, `ReadJSON`, `RequestID`, `Recover`, `Chain`), `internal/shared/sqlitex` (`Open`, `WithTx`), `internal/shared/config`.

Library yang dipakai (go.mod root): `github.com/golang-jwt/jwt/v5`, `modernc.org/sqlite`. Driver diimpor `_ "modernc.org/sqlite"`, nama driver `"sqlite"`.

## 3. Skema SQLite `auth.db`

### Tabel `users`
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `id` | TEXT | PRIMARY KEY | `USR-01`, `USR-02`, `ADM-01`, `DEV-01` |
| `email` | TEXT | NOT NULL UNIQUE | lowercase |
| `password` | TEXT | NOT NULL | **plaintext `123456` (khusus demo)** |
| `name` | TEXT | NOT NULL | |
| `role` | TEXT | NOT NULL CHECK(role IN ('CUSTOMER','ADMIN_SISTEM','ADMIN_DEV')) | `GUEST` tidak disimpan |
| `is_active` | INTEGER | NOT NULL DEFAULT 1 | 0 = tidak bisa login |
| `created_at` | TEXT | NOT NULL | RFC 3339 UTC |

### Tabel `login_audit`
| Kolom | Tipe | Constraint | Keterangan |
|---|---|---|---|
| `id` | INTEGER | PRIMARY KEY AUTOINCREMENT | |
| `user_id` | TEXT | NULL | NULL bila email tak dikenal |
| `email` | TEXT | NOT NULL | email yang dicoba |
| `success` | INTEGER | NOT NULL | 1/0 |
| `ip` | TEXT | NULL | `r.RemoteAddr` (host saja) |
| `created_at` | TEXT | NOT NULL | |

### Seed (idempoten: `INSERT OR IGNORE`, jalan saat start)
| `id` | `email` | `password` | `name` | `role` |
|---|---|---|---|---|
| `USR-01` | `budi@toko.com` | `123456` | Budi Santoso | `CUSTOMER` |
| `USR-02` | `siti@toko.com` | `123456` | Siti Rahma | `CUSTOMER` |
| `ADM-01` | `admin@toko.com` | `123456` | Admin Sistem | `ADMIN_SISTEM` |
| `DEV-01` | `dev@toko.com` | `123456` | Developer Toko | `ADMIN_DEV` |

Perbandingan password demo memakai `subtle.ConstantTimeCompare`.

## 4. Kontrak Endpoint Gateway (milik sendiri)

Semua respons mengikuti konvensi Master §7 (`{"data":...}` / `{"error":...}`, field `snake_case`).

### `POST /auth/login` — publik
Request:
```json
{ "email": "budi@toko.com", "password": "123456" }
```
| Field | Tipe | Validasi |
|---|---|---|
| `email` | string | wajib, format email, di-lowercase |
| `password` | string | wajib, 1–128 karakter |

Respons `200`:
```json
{
  "data": {
    "token": "<jwt>",
    "token_type": "Bearer",
    "expires_in": 28800,
    "user": { "id": "USR-01", "email": "budi@toko.com", "name": "Budi Santoso", "role": "CUSTOMER" }
  }
}
```
| HTTP | `code` | Kondisi |
|---|---|---|
| 400 | `VALIDATION_ERROR` | Body tidak valid |
| 401 | `INVALID_CREDENTIALS` | Email/password salah **atau** `is_active=0` (pesan sama) |

Setiap percobaan dicatat di `login_audit`.

### `GET /auth/demo-users` — publik
Dipakai `RoleSwitcher`. **Tidak** mengembalikan password.
```json
{ "data": [
  { "id": "USR-01", "email": "budi@toko.com", "name": "Budi Santoso", "role": "CUSTOMER" },
  { "id": "USR-02", "email": "siti@toko.com", "name": "Siti Rahma", "role": "CUSTOMER" },
  { "id": "ADM-01", "email": "admin@toko.com", "name": "Admin Sistem", "role": "ADMIN_SISTEM" },
  { "id": "DEV-01", "email": "dev@toko.com", "name": "Developer Toko", "role": "ADMIN_DEV" }
] }
```
Urutan: `CUSTOMER`, `ADMIN_SISTEM`, `ADMIN_DEV`, lalu `id` ascending. Hanya `is_active=1`.

### `GET /auth/me` — butuh token
`{ "data": { "id", "email", "name", "role" } }` dari klaim JWT (tanpa query DB). `401 UNAUTHENTICATED` bila tidak valid.

### `GET /health` — publik
`{ "data": { "status": "ok", "service": "service-gateway" } }`

### `GET /api/dev/health` — `ADMIN_DEV` saja
Memanggil `GET /health` pada catalog, order, ticket, mcp, agent secara paralel (goroutine + `errgroup`/`sync.WaitGroup`, timeout 3 s):
```json
{ "data": { "catalog": "up", "order": "up", "ticket": "down", "mcp": "up", "agent": "up" } }
```
Role lain → `403 FORBIDDEN`. (Service Go membalas `{"data":{"status":"ok",...}}`; MCP/agent cukup status 2xx.)

## 5. Spesifikasi JWT (`golang-jwt/jwt/v5`)
| Properti | Nilai |
|---|---|
| Algoritma | HS256 — `jwt.NewWithClaims(jwt.SigningMethodHS256, claims)` |
| Secret | `[]byte(JWT_SECRET)` |
| Masa berlaku | `JWT_EXPIRES_IN` (default `8h`, `time.ParseDuration`) |
| Klaim | `jwt.RegisteredClaims{Subject (=user id), IssuedAt, ExpiresAt}` + `email`, `name`, `role` |
| Parsing | `jwt.ParseWithClaims(..., jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())` — **wajib** membatasi metode (anti `alg=none`/konfusi algoritma) |
| Transport | `Authorization: Bearer <token>` |

## 6. Routing (`http.ServeMux`)
```go
mux := http.NewServeMux()
mux.HandleFunc("POST /auth/login", authH.Login)
mux.HandleFunc("GET /auth/demo-users", authH.DemoUsers)
mux.Handle("GET /auth/me", authn.Required(http.HandlerFunc(authH.Me)))
mux.HandleFunc("GET /health", healthH)
mux.Handle("GET /api/dev/health", authn.Required(requireRole("ADMIN_DEV")(devHealth)))

mux.Handle("/api/products",    catalogProxy);  mux.Handle("/api/products/",  catalogProxy)
mux.Handle("/api/orders",      orderProxy);    mux.Handle("/api/orders/",    orderProxy)
mux.Handle("/api/tickets",     ticketProxy);   mux.Handle("/api/tickets/",   ticketProxy)
mux.Handle("/api/knowledge/",  ticketProxy)                       // knowledge base ada di service-ticket
mux.Handle("/api/agent/",      agentProxy)                        // stripBrowserOrigin + identityRewrite
```

### Tabel route proxy
| Path publik | Target | Path ke upstream | Guard gateway |
|---|---|---|---|
| `/api/products/**` | `CATALOG_URL` | **sama persis** (`/api/products/...`) | Semua role (GUEST boleh); service domain menegakkan RBAC mutasi |
| `/api/orders/**` | `ORDER_URL` | sama persis | `CUSTOMER`, `ADMIN_SISTEM`, `ADMIN_DEV` (GUEST → `401 UNAUTHENTICATED`) |
| `/api/tickets/**` | `TICKET_URL` | sama persis | sama dengan orders |
| `/api/knowledge/**` | `TICKET_URL` | sama persis | Semua role (GUEST boleh baca/cari); mutasi dokumen dijaga service |
| `/api/agent/**` | `AGENT_URL` | **hapus prefiks** `/api/agent` (mis. `/api/agent/run` → `/run`) | Semua role termasuk GUEST |

> Karena path publik = path upstream untuk domain services, **tidak ada path rewrite** kecuali agent. Path `/internal/**` tidak pernah dipetakan di mux gateway.

## 7. Middleware (urutan, `httputil.Chain`)
| # | Middleware | Perilaku |
|---|---|---|
| 1 | `httputil.Recover` | `recover()` → `500 INTERNAL_ERROR` (tanpa stack ke klien; stack ke log) |
| 2 | `httputil.RequestID` | Ambil `x-request-id` klien bila UUID valid, jika tidak buat baru (`crypto/rand`); simpan di `context`, set di header respons |
| 3 | `cors` | Origin == `CORS_ORIGIN` → set `Access-Control-Allow-Origin/Methods/Headers` (`Authorization, Content-Type, x-request-id`) & `Vary: Origin`; `OPTIONS` preflight → `204` |
| 4 | `sanitizeIdentityHeaders` | `r.Header.Del` untuk `x-user-id`, `x-user-role`, `x-internal-api-key`, `x-service-name` |
| 5 | `blockInternalPaths` | `path.Clean(r.URL.Path)` (setelah decode) mengandung segmen `internal` → `404 NOT_FOUND` |
| 6 | `authenticate` (opsional-JWT) | Tanpa `Authorization` → identitas `GUEST`/`GUEST`. Ada tapi tidak valid/kedaluwarsa → `401 UNAUTHENTICATED` (**tidak** diturunkan jadi GUEST). Valid → identitas ke `context` |
| 7 | `requireRole(...)` | Guard per route sesuai tabel §6 (fail-fast; service domain tetap memvalidasi ulang) |
| 8 | proxy handler | §8 |

Untuk route yang diproxy, body **tidak dibaca** (kecuali identityRewrite agent) agar stream utuh. Batas body 1 MB via `http.MaxBytesReader` hanya pada route `/auth/*` dan identityRewrite.

## 8. Reverse Proxy — `httputil.NewSingleHostReverseProxy`
```go
func newProxy(target *url.URL, opt Options) http.Handler {
    rp := httputil.NewSingleHostReverseProxy(target)
    base := rp.Director
    rp.Director = func(r *http.Request) {
        base(r)                                   // set scheme/host & gabung path
        if opt.StripPrefix != "" { r.URL.Path = strings.TrimPrefix(r.URL.Path, opt.StripPrefix); r.URL.RawPath = "" }
        id := identity.From(r.Context())          // dari middleware authenticate
        r.Header.Set("x-user-id", id.UserID)
        r.Header.Set("x-user-role", id.Role)
        r.Header.Set("x-internal-api-key", cfg.InternalAPIKey)
        r.Header.Set("x-request-id", id.RequestID)
        r.Header.Del("Authorization")             // JWT TIDAK diteruskan
        if opt.StripBrowserOrigin { r.Header.Del("Origin"); r.Header.Del("Referer") }
        r.Host = target.Host
    }
    rp.Transport = &http.Transport{ ResponseHeaderTimeout: opt.Timeout, /* pool default */ }
    rp.FlushInterval = -1                         // flush segera: wajib untuk SSE /run_sse
    rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
        // errors.Is(err, context.DeadlineExceeded) atau net.Error.Timeout() → 504 UPSTREAM_TIMEOUT
        // selain itu → 502 UPSTREAM_ERROR  (format error standar via httputil.WriteError)
    }
    return rp
}
```
| Parameter | Nilai |
|---|---|
| Timeout domain services | `UPSTREAM_TIMEOUT_MS` (10 s) sebagai `ResponseHeaderTimeout` |
| Timeout agent | 120 s |
| Catatan Go | `NewSingleHostReverseProxy` menambah `X-Forwarded-For`; header hop-by-hop dibersihkan otomatis. Header HTTP case-insensitive: `Header.Set("x-user-id", …)` dikanonikalkan menjadi `X-User-Id` di kabel — service lain **wajib** membaca via `r.Header.Get("x-user-id")` |
| Respons upstream | Diteruskan apa adanya (status + body) |
| `ModifyResponse` | Tidak dipakai; CORS ditangani middleware sebelum proxy (upstream tidak boleh set CORS) |

## 9. `/api/agent` — Perlakuan Khusus

### 9.1 `stripBrowserOrigin`
ADK `api_server` menolak request ber-header `Origin`. Pada `Director` agent: `r.Header.Del("Origin")` dan `r.Header.Del("Referer")` (opsi `StripBrowserOrigin: true` hanya untuk `agentProxy`). CORS terhadap **browser** sudah diselesaikan oleh middleware `cors` Gateway **sebelum** header dihapus.

### 9.2 Penyatuan identitas user ↔ session ADK (`agent_identity.go`)
Gateway tidak mempercayai `userId` dari klien:
| Request ke Gateway | Aksi Gateway |
|---|---|
| `POST /api/agent/apps/{app}/users/{userId}/sessions` **dan** `…/sessions/{sessionId}` | `r.PathValue("userId")` ≠ identitas → `403 FORBIDDEN`. Baca body (maks 1 MB; kosong → `{}`), decode ke `map[string]any`, set `state["user_id"]`, `state["user_role"]` (menimpa), encode ulang, set `r.Body`, `r.ContentLength`, header `Content-Length` |
| `POST /api/agent/run`, `/api/agent/run_sse` | `body.userId` (camelCase — protokol ADK) ≠ `x-user-id` → `403`. Hapus `user_id`/`user_role` dari `body.stateDelta` jika ada. Tulis ulang body |
| `PATCH /api/agent/apps/{app}/users/{userId}/sessions/{sessionId}` | `{userId}` harus = identitas; `body.stateDelta` memuat `user_id`/`user_role` → `403` |
| `GET/DELETE /api/agent/apps/{app}/users/{userId}/sessions/...` | `{userId}` harus = identitas |
| Lainnya (`/docs`, `/openapi.json`, `/list-apps`, `/debug/*`, `/dev-ui`) | `404 NOT_FOUND` di Gateway |

GUEST memakai `userId="GUEST"`. **Defense in depth:** service-agent menegakkan aturan yang sama sendiri (`idguard`, lihat `06-SERVICE-AGENT.md` §6.1) — hasil spike ADK Go menunjukkan rute `PATCH …/sessions/{sid}` dan `POST …/sessions` (tanpa id) juga dapat menulis state, sehingga keduanya wajib tercakup. Header `x-user-id`/`x-user-role` tetap diteruskan ke ADK sebagai cadangan.
> Field `appName`, `userId`, `sessionId`, `newMessage`, `stateDelta` di jalur `/api/agent/**` adalah kontrak **protokol ADK (camelCase)** — satu-satunya pengecualian dari konvensi `snake_case` (Master §7).

## 10. Standar Respons Error Gateway
| Situasi | HTTP | `code` |
|---|---|---|
| Token invalid/expired | 401 | `UNAUTHENTICATED` |
| Role tidak diizinkan | 403 | `FORBIDDEN` |
| `/internal` atau path agent tak diekspos | 404 | `NOT_FOUND` |
| Service tujuan mati/ECONNREFUSED | 502 | `UPSTREAM_ERROR` |
| Timeout | 504 | `UPSTREAM_TIMEOUT` |
| Method tidak sesuai pola mux | 405 | `METHOD_NOT_ALLOWED` (`ServeMux` otomatis memberi 405; bungkus agar JSON standar) |

Handler 404 default mux juga dibungkus agar mengembalikan JSON error standar.

## 11. Bootstrap `main.go`
1. `config.Load()` (fail-fast variabel wajib).
2. `sqlitex.Open(AUTH_DB_PATH)` → migrate → seed.
3. Bangun `http.Server{Addr: ":"+GATEWAY_PORT, Handler: chain, ReadHeaderTimeout: 5s, IdleTimeout: 60s}` (**tanpa** `WriteTimeout` agar SSE tidak terputus).
4. `signal.NotifyContext(SIGINT, SIGTERM)` → `srv.Shutdown(ctx 10s)`.

## 12. Kriteria Penerimaan
- [ ] `go build ./service-gateway` tanpa CGO (`CGO_ENABLED=0`) berhasil.
- [ ] `POST /auth/login` 4 akun demo → token valid; password salah → `401 INVALID_CREDENTIALS`.
- [ ] Token dengan `alg=none` atau HS512 → `401`.
- [ ] `GET /auth/demo-users` tidak memuat field `password`.
- [ ] Tanpa token: `/api/orders` → `401`; `/api/products` → `200`; `/api/knowledge/search?q=retur` → `200`.
- [ ] Header `x-user-role: ADMIN_SISTEM` palsu dari klien tidak berpengaruh.
- [ ] Service domain tidak menerima header `Authorization`.
- [ ] `/api/products/internal/...` dan `/internal/...` → `404`.
- [ ] Request `/api/agent/run` dengan `Origin` browser berhasil (tidak ditolak ADK); SSE tidak ter-buffer.
- [ ] `body.userId` ≠ identitas JWT → `403`.
- [ ] `GET /api/dev/health` hanya untuk `ADMIN_DEV`.
