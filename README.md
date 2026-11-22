# toko-ai-microservices

Toko elektronik & aksesoris berbasis **microservices** dengan **asisten AI multi-agent** (Google ADK + Gemini) dan **pencarian semantik / RAG**.

Pelanggan bisa mengobrol dengan asisten untuk mencari produk ("mouse yang tidak bikin pergelangan pegal"), menanyakan kebijakan toko, membuat dan membayar pesanan, serta membuat tiket komplain. Semua aksi lewat jalur yang sama dengan antarmuka web, jadi aturan akses dan validasi tetap berlaku.

> Spesifikasi lengkap ada di [`docs/prd/`](docs/prd/00-MASTER-PRD.md).

## Daftar isi
- [Fitur utama](#fitur-utama)
- [Arsitektur](#arsitektur)
- [Daftar service](#daftar-service)
- [Mulai cepat](#mulai-cepat)
- [Konfigurasi](#konfigurasi)
- [Menjalankan dengan Docker](#menjalankan-dengan-docker)
- [Pengujian](#pengujian)
- [Struktur folder](#struktur-folder)
- [Keamanan](#keamanan)
- [Dokumentasi](#dokumentasi)
- [Batasan yang diketahui](#batasan-yang-diketahui)

## Fitur utama
- **Asisten AI multi-agent:** satu *orchestrator* yang merutekan ke dua spesialis, yaitu katalog (`shopping_catalog_agent`) dan pesanan/dukungan (`order_support_agent`).
- **Pencarian semantik:** produk dan knowledge base dicari berdasarkan makna, bukan hanya kata kunci. Tanpa API key Gemini, pencarian otomatis memakai fallback keyword.
- **Knowledge base (RAG):** admin mengelola dokumen kebijakan. Agent menjawab pertanyaan kebijakan hanya dari dokumen itu.
- **Alur belanja lengkap:** keranjang, checkout, pembayaran (simulasi), pembatalan, kedaluwarsa otomatis, dan pengembalian stok bila bayar gagal.
- **Tiket komplain** dengan siklus status dan percakapan.
- **Empat peran:** `GUEST`, `CUSTOMER`, `ADMIN_SISTEM`, `ADMIN_DEV`.
- **Aksi tulis dengan konfirmasi dua langkah:** agent menampilkan ringkasan dan menunggu persetujuan eksplisit sebelum membuat pesanan, membayar, membatalkan, atau membuat tiket.

## Arsitektur
Hanya **gateway** yang terbuka ke luar. Service lain hanya bisa dijangkau dari dalam jaringan.

```
Browser ──► Frontend (:3000)
              │
              ▼
          Gateway (:8080) ── login JWT, CORS, sanitasi header, reverse proxy
              │
   ┌──────────┼───────────┬───────────────┐
   ▼          ▼           ▼               ▼
Catalog     Order       Ticket          Agent (:8000)
(:4001)    (:4002)   + Knowledge        Orchestrator
   ▲          │        (:4003)           ├─ shopping_catalog_agent
   │          ▼           ▲              └─ order_support_agent
   └──────────┘           │                    │
                          │                    ▼
                          └────────────── MCP (:5000) ◄── 17 tool
                                          (identitas per request)
```

Alur singkat:
1. Gateway memverifikasi JWT, lalu meneruskan identitas ke service lain lewat header internal yang tidak bisa dipalsukan klien.
2. Agent tidak memanggil service domain secara langsung. Ia memakai tool di `service-mcp`, yang meneruskan identitas pengguna ke catalog, order, dan ticket.
3. Setiap service tetap memeriksa peran dan kepemilikan data sendiri, sehingga akses tidak bergantung pada AI.

## Daftar service
| Service | Port | Stack | Fungsi |
|---|---|---|---|
| `service-gateway` | 8080 | Go `net/http` | Login/JWT (HS256), CORS, sanitasi header, blokir `/internal`, reverse proxy, proteksi rute agent, `/api/dev/health` |
| `service-catalog` | 4001 | Go `net/http` | CRUD produk, stok atomik, embedding, `semantic-search`, seed 14 produk |
| `service-order` | 4002 | Go `net/http` | State machine pesanan, simulasi bayar, stok via catalog (dengan kompensasi), job kedaluwarsa, seed 8 pesanan |
| `service-ticket` | 4003 | Go `net/http` | Tiket + RBAC, knowledge base (chunking, embedding, pencarian), seed 5 tiket dan 8 dokumen |
| `service-mcp` | 5000 | Go + `go-sdk` MCP | 17 tool, stateless, identitas per request |
| `service-agent` | 8000 | Go + Google ADK | Orchestrator + 2 sub-agent, toolset per identitas, guard anti-halusinasi (H2/H4/H11) |
| `service-frontend` | 3000 | Nuxt 3 SPA (`ssr:false`) | Katalog, detail produk, keranjang, checkout, pesanan, tiket, panel admin, chat |

Setiap service punya database SQLite sendiri (pure-Go, tanpa CGO), dibuat otomatis di `./data/`.

## Mulai cepat
**Prasyarat:** Go 1.26+, Node.js 20+, dan (opsional) Docker.

```bash
# 1. Salin konfigurasi
cp .env.example .env
#    Isi GOOGLE_API_KEY (wajib untuk agent, opsional untuk embedding)

# 2. Jalankan tiap service di terminal terpisah (dari root repo)
go run ./service-catalog     # :4001
go run ./service-order       # :4002
go run ./service-ticket      # :4003
go run ./service-mcp         # :5000
go run ./service-agent       # :8000  (butuh GOOGLE_API_KEY)
go run ./service-gateway     # :8080

# 3. Frontend
cd service-frontend && npm install && npm run dev   # :3000
```

Buka <http://localhost:3000>. Tanpa `GOOGLE_API_KEY`, semua service kecuali agent tetap berjalan, dan pencarian memakai fallback keyword.

### Akun demo
Semua memakai password `123456`.

| Email | Peran |
|---|---|
| `budi@toko.com` | CUSTOMER |
| `siti@toko.com` | CUSTOMER |
| `admin@toko.com` | ADMIN_SISTEM |
| `dev@toko.com` | ADMIN_DEV |

### Contoh percakapan dengan asisten
- *"Cari powerbank di bawah 400 ribu."*
- *"Mouse yang tidak bikin pergelangan tangan pegal."*
- *"Bagaimana kebijakan refund?"*
- *"Status pesanan ORD-003 bagaimana?"* (perlu login)
- *"Pesankan 2 PRD-003, bayar pakai e-wallet, kirim ke Jl. Merdeka No. 10."* (agent meminta konfirmasi dulu)

## Konfigurasi
Semua konfigurasi lewat environment variable. Prioritas: **environment proses > `service-x/.env` > `.env` (root)**.

| File | Isi |
|---|---|
| `.env.example` | Gabungan semua service (untuk dijalankan dari root) |
| `service-*/.env.example` | Hanya variabel milik service tersebut |

Variabel yang paling penting:

| Variabel | Fungsi |
|---|---|
| `INTERNAL_API_KEY` | Rahasia bersama antar-service |
| `JWT_SECRET` | Penanda tangan token (hanya dipegang gateway) |
| `GOOGLE_API_KEY` | Kunci Gemini untuk agent dan embedding |
| `AGENT_MODEL`, `EMBEDDING_MODEL` | Model yang dipakai |
| `SEMANTIC_MIN_SCORE` | Ambang kemiripan pencarian semantik |
| `*_PORT`, `*_URL`, `*_DB_PATH` | Port, alamat antar-service, lokasi database |

> **Penting:** nilai bawaan `INTERNAL_API_KEY` dan `JWT_SECRET` hanya untuk demo. Ganti dengan nilai acak sebelum dipakai di luar mesin lokal.

## Menjalankan dengan Docker
```bash
cp .env.example .env              # isi GOOGLE_API_KEY
docker compose up --build -d      # semua service
docker compose down               # menghentikan service (data tetap ada)
```
- Hanya **frontend (:3000)** dan **gateway (:8080)** yang dibuka ke host. Service lain hanya ada di jaringan Docker.
- **Lokasi database:** file SQLite disimpan di folder biasa di host, yaitu `./data/<service>/*.db` (`gateway/auth.db`, `catalog/catalog.db`, `order/order.db`, `ticket/ticket.db`). Buka dengan DB Browser for SQLite atau ekstensi SQLite di VS Code, sebaiknya dalam mode baca saja. Untuk mereset data ke awal, hentikan service lalu hapus folder `data/`.
- Tiap service punya `Dockerfile` multi-stage dengan user non-root dan healthcheck. Build context-nya root repo (frontend memakai `service-frontend/`).
- File `.env` tidak ikut masuk ke image (lihat `.dockerignore`).

## Pengujian
```bash
go test ./...                 # tes unit/integrasi per service (tanpa jaringan eksternal)
bash scripts/e2e/run.sh       # e2e: binary nyata saling terhubung lewat gateway
```
- `scripts/e2e/run.sh` menjalankan catalog, order, ticket, mcp, dan gateway tanpa mock, memakai database sementara di `data/e2e/`. Service agent tidak ikut karena butuh LLM.
- Skrip e2e saat ini memanggil `./bin/service-*.exe`, jadi cocok untuk Windows. Di Linux/macOS, hapus ekstensi `.exe` di skrip.

Build binary manual:
```bash
CGO_ENABLED=0 go build -o bin/ ./service-gateway ./service-catalog ./service-order ./service-ticket ./service-mcp ./service-agent
```

## Struktur folder
Backend Go memakai **satu module di root**, jadi kode bersama mudah dibagi.

```
go.mod                       # module toko-ai-microservices
internal/shared/             # kode bersama: httputil, idguard, sqlitex, validate,
                             #   svcclient, embedding, vector, config
service-<nama>/main.go       # entrypoint tiap service
service-<nama>/internal/     # config, store, service, handlers / tools / agents
service-<nama>/.env.example  # env milik service itu
service-frontend/            # Nuxt 3 (Node ≥ 20)
scripts/e2e/                 # tes end-to-end dengan binary sungguhan
docs/prd/                    # spesifikasi per service
docker-compose.yml
.env.example                 # env gabungan semua service
```

Kode di `internal/shared` disalin ke tahap build tiap service lalu di-compile ke dalam binary-nya, jadi image akhir hanya berisi satu file binary.

## Keamanan
- **Identitas tidak datang dari AI atau pesan pengguna.** Identitas diisi server dan dipaksakan oleh `idguard`. Argumen identitas dari LLM dibuang sebelum tool dipanggil.
- **Header identitas dari klien diabaikan** oleh gateway; hanya token JWT yang dipercaya.
- **Rute `/internal` diblokir** di gateway, dan antar-service memakai `x-internal-api-key`.
- **Isolasi data:** pelanggan hanya melihat pesanan dan tiket miliknya. Dokumen knowledge nonaktif hanya terlihat oleh admin.
- **Guard di callback agent:** ID entitas harus pernah muncul di hasil tool (H2), klaim aksi sukses harus didukung tool yang sukses (H4), dan jawaban kebijakan harus berasal dari knowledge base (H11).
- Isi hasil tool diperlakukan sebagai data, bukan instruksi, untuk mengurangi risiko prompt injection.

## Dokumentasi
Spesifikasi per service ada di `docs/prd/`:

| Dokumen | Isi |
|---|---|
| [`00-MASTER-PRD.md`](docs/prd/00-MASTER-PRD.md) | Gambaran umum, peran, kontrak API, variabel env |
| [`01-SERVICE-GATEWAY.md`](docs/prd/01-SERVICE-GATEWAY.md) | Gateway |
| [`02-SERVICE-CATALOG.md`](docs/prd/02-SERVICE-CATALOG.md) | Katalog dan pencarian semantik |
| [`03-SERVICE-ORDER.md`](docs/prd/03-SERVICE-ORDER.md) | Pesanan |
| [`04-SERVICE-TICKET.md`](docs/prd/04-SERVICE-TICKET.md) | Tiket dan knowledge base |
| [`05-SERVICE-MCP.md`](docs/prd/05-SERVICE-MCP.md) | Tool MCP |
| [`06-SERVICE-AGENT.md`](docs/prd/06-SERVICE-AGENT.md) | Agent dan guard |
| [`07-SERVICE-FRONTEND.md`](docs/prd/07-SERVICE-FRONTEND.md) | Frontend |

## Batasan yang diketahui
- Kualitas agent dengan **Gemini asli** baru diuji sebagian (2 skenario; kuota gratis habis). Rekomendasi semantik, alur tulis dua langkah (buat pesanan/tiket), dan kalibrasi `SEMANTIC_MIN_SCORE=0.55` dengan embedding asli masih perlu dicoba.
- Halaman frontend lolos typecheck dan build, tetapi belum diuji manual di peramban; hanya kontrak API yang diverifikasi dengan curl.
- Pola regex guard H4/H11 bersifat heuristik dan perlu dipantau terhadap false positive pada percakapan nyata.
- Sesi percakapan agent disimpan di memori (`session.InMemoryService()`), jadi hilang saat `service-agent` dimulai ulang.
- Pembayaran masih simulasi, dan SQLite per service cocok untuk demo atau beban kecil, bukan untuk banyak replika.
