# 07 — TRD `service-frontend`

| Item | Nilai |
|---|---|
| Port | `3000` (`FRONTEND_PORT`) |
| Stack | Nuxt 3 + TypeScript + Tailwind CSS (`@nuxtjs/tailwindcss`), Pinia (opsional untuk cart) |
| Mode render | **SPA murni: `ssr: false` (global) di `nuxt.config.ts`** — tidak ada render server, tidak ada server routes/Nitro API |
| Backend | **Hanya** `service-gateway` Go di `http://localhost:8080` (`NUXT_PUBLIC_API_BASE`), dipanggil **langsung dari browser** (CORS ditangani Gateway) |
| Penyimpanan token | JWT di **`localStorage`** (kunci `toko_token`) |
| Rujukan | `00-MASTER-TRD.md` (§7 konvensi JSON — wajib identik dengan backend Go) |

## 1. Prinsip
1. Tidak ada form login manual. Autentikasi lewat **RoleSwitcher** (klik akun demo → login otomatis dengan password demo `123456`).
2. **SPA (`ssr: false`)**: seluruh kode berjalan di browser sehingga JWT di `localStorage` dapat dibaca sinkron saat start; tidak ada hydration mismatch, tidak ada kebutuhan cookie/`useCookie`, dan **tidak ada perantara server** — browser → Gateway langsung.
3. Akses `localStorage` selalu dibungkus `try/catch` (mode privat/blocked) dengan fallback state in-memory.
4. Guard frontend hanya UX; keamanan sebenarnya di backend.
5. Uang tampil `Rp299.000` (`Intl.NumberFormat('id-ID')`); waktu RFC 3339 UTC dari API ditampilkan lokal (`Asia/Jakarta`).
6. Semua error API tampil dari `error.message`; `error.request_id` ditampilkan kecil untuk debugging.
7. **Seluruh tipe & nama field mengikuti kontrak backend (`snake_case`)**; tidak ada konversi ke camelCase di lapisan data (komponen memakai `product.stock_status`, `order.total_amount`, dst.). Satu-satunya payload camelCase adalah protokol ADK pada `/api/agent/**`.

## 2. Peta Halaman & Akses

| Route | Halaman | GUEST | CUSTOMER | ADMIN_SISTEM | ADMIN_DEV |
|---|---|:-:|:-:|:-:|:-:|
| `/` | Beranda + katalog (pencarian eksak & **pencarian AI semantik**) | ✅ | ✅ | ✅ | ✅ |
| `/products/[id]` | Detail produk | ✅ | ✅ (+keranjang) | ✅ | ✅ |
| `/help` | **Pusat Bantuan**: cari kebijakan toko (knowledge base) | ✅ | ✅ | ✅ | ✅ |
| `/cart` | Keranjang (`localStorage`) | ✅ | ✅ | — | — |
| `/checkout` | Checkout | → prompt pilih akun | ✅ | — | — |
| `/orders` | Pesanan saya | → redirect `/` | ✅ | → `/admin/orders` | → `/dev` |
| `/orders/[id]` | Detail + bayar/batalkan | ❌ | ✅ (milik sendiri) | ✅ (read) | ✅ (read) |
| `/tickets` | Tiket saya | ❌ | ✅ | → `/admin/tickets` | → `/dev` |
| `/tickets/new` | Buat tiket (`?order_id=`) | ❌ | ✅ | ❌ | ❌ |
| `/tickets/[id]` | Detail + percakapan | ❌ | ✅ (milik sendiri) | ✅ | ✅ |
| `/admin` | Dashboard admin | ❌ | ❌ | ✅ | ❌ |
| `/admin/products` | CRUD produk + stok + tombol **Reindex embedding** | ❌ | ❌ | ✅ | read-only |
| `/admin/orders` | Semua pesanan + fulfilment | ❌ | ❌ | ✅ | read-only |
| `/admin/tickets` | Semua tiket + ubah status | ❌ | ❌ | ✅ | read-only kecuali `TECHNICAL` |
| `/admin/knowledge` | **Kelola Knowledge Base** (dokumen kebijakan) + **Reindex** | ❌ | ❌ | ✅ | ❌ (baca saja via `/help`) |
| `/dev` | Dashboard developer: health service, tiket `TECHNICAL`, panel JWT | ❌ | ❌ | ❌ | ✅ |

Route middleware (`middleware/auth.global.ts`, client-only):
- Role tidak berhak → redirect `/` + toast "Akses ditolak".
- Tanpa token pada rute proteksi → buka RoleSwitcher dan simpan `redirect`.
- `401 UNAUTHENTICATED` → `logout()` + toast "Sesi berakhir".

### 2.1 Spesifikasi halaman utama
| Halaman | Elemen & perilaku | API |
|---|---|---|
| `/` | Grid produk, search `q`, filter `category`, harga, `in_stock`, sort, paginasi. **Toggle "Pencarian AI"**: aktif → input menjadi kotak bahasa natural ("mouse yang tidak bikin pergelangan pegal") dan memanggil semantic search; hasil menampilkan alasan kemiripan berupa badge "Cocok" (skor dikonversi: ≥0.75 "Sangat cocok", ≥0.55 "Cocok"; angka mentah tidak ditampilkan). `meta.mode="keyword_fallback"` → banner "Pencarian cerdas sedang terbatas". `data: []` → empty state "Belum ada produk yang cocok". Badge `stock_status` (`Habis`/`Stok menipis`/`Tersedia`); tombol keranjang disabled bila stok 0 | `GET /api/products`, `GET /api/products/semantic-search?q=&limit=` |
| `/products/[id]` | Detail (+`specifications`), kuantitas, tombol "Tanya Asisten AI tentang produk ini" (membuka chat dengan `initialPrompt`) | `GET /api/products/{id}` |
| `/help` | Kotak pencarian + saran ("syarat retur", "garansi", "estimasi pengiriman", "pembayaran gagal"). Hasil: kartu chunk (judul dokumen, kategori, `content`); tombol "Lihat dokumen lengkap" (`GET /api/knowledge/documents/{id}`); tombol "Tanya Asisten AI"; tombol "Buat tiket" (login diperlukan). Daftar dokumen per kategori (`GET /api/knowledge/documents`). `data: []` → "Tidak ada informasi resmi; Anda dapat membuat tiket" | `GET /api/knowledge/search?q=`, `GET /api/knowledge/documents` |
| `/cart` | Ubah qty/hapus; total estimasi; catatan harga final dari server | — |
| `/checkout` | `shipping_address` (10–300), `payment_method`. Submit → `/orders/[id]` (hitung mundur `payment_expires_at`). `409 INSUFFICIENT_STOCK` → tampilkan `details.product_id/available/requested` | `POST /api/orders` |
| `/orders/[id]` | Status + timeline `history`, item, `failure_reason`; tombol **Bayar (sukses)**, panel Demo: **Simulasi gagal/timeout**, **Batalkan** (`PENDING_PAYMENT`), **Buat tiket** → `/tickets/new?order_id=`; untuk `PAYMENT_FAILED`/`PAYMENT_TIMEOUT` tampil tautan "Baca kebijakan pembayaran" → `/help?q=pembayaran gagal` | `GET /api/orders/{id}`, `POST .../pay`, `POST .../cancel` |
| `/tickets/new` | `subject`, `description`, `category`, `priority`, `order_id` (dropdown). Sebelum submit, bila kategori `PAYMENT/SHIPPING/PRODUCT`: tampilkan panel "Mungkin ini menjawab pertanyaan Anda" berisi 2 hasil `knowledge/search` dari `subject` (opsional, tidak memblokir submit) | `POST /api/tickets` |
| `/tickets/[id]` | Info + thread, balasan, Reopen/Tutup | `GET/POST /api/tickets/{id}...` |
| `/admin/products` | Tabel + modal (`name`, `description`, `specifications`, `category`, `price`, `stock`, `image_url`); "Atur stok"; nonaktifkan; **Reindex embedding** | `POST/PUT/DELETE /api/products`, `PATCH .../stock`, `POST /api/products/reindex` |
| `/admin/knowledge` | Tabel dokumen (id, judul, kategori, aktif, `chunk_count`), editor (judul, kategori, `content` Markdown), nonaktifkan, **Reindex** | `GET/POST/PUT/DELETE /api/knowledge/documents`, `POST /api/knowledge/reindex` |
| `/admin/orders`, `/admin/tickets` | Seperti sebelumnya (fulfilment; ubah status, `resolution_note`) | `PATCH /api/orders/{id}/status`, `PATCH /api/tickets/{id}/status` |
| `/dev` | Kartu status, tiket `TECHNICAL`, panel "Identitas JWT" (`/auth/me`), contoh `curl` | `GET /api/dev/health` |

## 3. Composables

### 3.1 `composables/useAuth.ts`
```ts
type Role = 'GUEST' | 'CUSTOMER' | 'ADMIN_SISTEM' | 'ADMIN_DEV'
interface AuthUser { id: string; email: string; name: string; role: Exclude<Role,'GUEST'> }

export function useAuth(): {
  user: Ref<AuthUser | null>
  token: Ref<string | null>               // mirror localStorage['toko_token']
  role: ComputedRef<Role>                 // 'GUEST' bila user null
  userId: ComputedRef<string>             // 'GUEST' bila user null
  isAuthenticated: ComputedRef<boolean>
  demoUsers: Ref<AuthUser[]>
  loadDemoUsers(): Promise<void>          // GET /auth/demo-users → res.data
  loginAs(email: string): Promise<void>   // POST /auth/login {email, password:'123456'} → res.data.{token,user}
  logout(): void                          // hapus localStorage & state, reset chat
  hasRole(...roles: Role[]): boolean
}
```
Perilaku:
- Penyimpanan: `localStorage.setItem('toko_token', token)` (try/catch). State reaktif dibagikan lewat `useState('auth')`.
- Start (`app.vue` `onMounted`/plugin client): bila token ada → `GET /auth/me` → `res.data` → `user`; `401` → `logout()`.
- Sinkron antar-tab: listener `storage` pada kunci `toko_token` → perbarui/clear state.
- Password demo `123456` hanya sebagai konstanta `DEMO_PASSWORD` di file ini.

### 3.2 `composables/useApi.ts`
```ts
interface ApiMeta { total?: number; page?: number; limit?: number; mode?: 'semantic'|'keyword_fallback'; query?: string; min_score?: number }
class ApiError extends Error { status: number; code: string; details: unknown; request_id: string }

export function useApi(): {
  $api<T>(path: string, opts?: { method?, body?, query?, headers?, signal? }): Promise<T>        // mengembalikan body.data
  $apiList<T>(path: string, opts?): Promise<{ data: T[]; meta: ApiMeta }>                         // untuk respons daftar
}
```
Aturan:
- Base URL = `useRuntimeConfig().public.apiBase`; request memakai `$fetch` (ofetch) langsung ke Gateway.
- Menyuntik `Authorization: Bearer <token>` bila ada. **Tidak pernah** mengirim `x-user-*`/`x-internal-api-key`.
- Sukses: unwrap `{"data": T}` → `T`; `204` → `undefined`.
- Error: baca `{"error":{code,message,details,request_id}}` → `throw new ApiError(...)`; jaringan gagal → `ApiError{status:0, code:'NETWORK_ERROR'}`.
- `401 UNAUTHENTICATED` saat ada token → `logout()` + `navigateTo('/')`.
- Header `Content-Type: application/json` untuk body. Header `x-request-id` opsional (UUID dari `crypto.randomUUID()`) agar korelasi log Gateway↔browser.

Helper domain (tipis, tipe dari `types/api.ts`):
| Fungsi | Request |
|---|---|
| `products.list(q)` | `GET /api/products?...` |
| `products.semanticSearch(q, opts)` | `GET /api/products/semantic-search?q=...&limit=5` |
| `knowledge.search(q, opts)` | `GET /api/knowledge/search?q=...&limit=3` |
| `orders.create/pay/cancel`, `tickets.create/...` | sesuai TRD 03/04 |

### 3.3 `composables/useChat.ts`
```ts
useChat(): {
  messages: Ref<ChatMessage[]>; sessionId: Ref<string|null>; loading: Ref<boolean>; error: Ref<string|null>
  open: Ref<boolean>
  send(text: string): Promise<void>
  reset(): void
}
```
Alur `send` (jalur `/api/agent/**` memakai **protokol ADK camelCase**, bukan envelope `{data}`):
1. Tanpa `sessionId` → buat `s-<uuid>`; `POST /api/agent/apps/toko_orchestrator/users/{userId}/sessions/{sessionId}` body `{}` (Gateway menyisipkan `state.user_id/user_role`). `userId` = `useAuth().userId` (`GUEST` bila tamu).
2. `POST /api/agent/run` body `{"appName":"toko_orchestrator","userId":"…","sessionId":"…","newMessage":{"role":"user","parts":[{"text":"…"}]},"streaming":false}`.
3. Respons = array event ADK; ambil teks final dari event terakhir milik agent (`content.parts[].text`).
4. `403` (userId mismatch) → `reset()` + buat session baru. Error lain → "Maaf, asisten sedang bermasalah. Coba lagi."
5. `userId` berubah (ganti akun) → `reset()` otomatis (watch).

## 4. Komponen

### 4.1 `components/RoleSwitcher.vue` (Quick Role Switcher)
| Aspek | Spesifikasi |
|---|---|
| Lokasi | Navbar kanan atas (dropdown); modal otomatis saat butuh login |
| Tampilan awal | "Tamu (GUEST)" + 4 akun demo dari `GET /auth/demo-users`, dikelompokkan per role (CUSTOMER biru, ADMIN_SISTEM oranye, ADMIN_DEV ungu) |
| Aksi | Klik akun → `loginAs(email)` → toast "Masuk sebagai Budi (CUSTOMER)" → redirect default (`CUSTOMER`→`/`, `ADMIN_SISTEM`→`/admin`, `ADMIN_DEV`→`/dev`) kecuali ada `redirect` |
| "Keluar (jadi Tamu)" | `logout()` |
| Tanpa form | **Tidak ada input email/password**; password tidak ditampilkan |
| State | loading per item; toast error `INVALID_CREDENTIALS`/jaringan |
| Aksesibilitas | `role="menu"`, keyboard, `aria-current` |
| Label | "Mode demo — akun contoh, tanpa password" |

### 4.2 `components/ChatWidget.vue` (Floating/Embedded AI Chat)
| Aspek | Spesifikasi |
|---|---|
| Mode | **Floating** (tombol kanan-bawah → panel 380×560; mobile penuh) atau **Embedded** (`mode="embedded"`) |
| Props | `mode`, `initialPrompt?` |
| Isi | Header ("Asisten Toko AI" + chip identitas), bubble pesan (markdown ringan), indikator mengetik, input (Enter kirim, Shift+Enter baris baru), "Percakapan baru" |
| Saran cepat | GUEST: "Cari powerbank di bawah 400 ribu", "Mouse yang tidak bikin pergelangan pegal", "Syarat retur barang?". CUSTOMER: "Status pesanan saya", "Kenapa pesanan ORD-003 gagal?", "Berapa lama garansi?", "Buat tiket komplain" |
| GUEST | Bisa chat (katalog + kebijakan); bila agent meminta login → tombol "Pilih akun demo" |
| Rendering ID | `ORD-\d+`/`TKT-\d+`/`PRD-\d+`/`KB-\d+` → tautan ke `/orders/..`, `/tickets/..`, `/products/..`, `/help?doc=KB-..` |
| Batas | Input maks 1000 karakter; kirim disabled saat `loading`; timeout 120 s |
| Persistensi | `messages` & `sessionId` per `userId` di `sessionStorage` (try/catch) |
| Keamanan | Teks agent dirender tanpa `v-html` mentah (sanitasi/markdown aman) |
| Dipasang | `layouts/default.vue` (floating) |

### 4.3 Komponen pendukung
`ProductCard.vue`, `StockBadge.vue`, `MatchBadge.vue` (hasil semantik), `SearchModeToggle.vue` (eksak/AI), `PolicyCard.vue` (chunk kebijakan), `OrderStatusBadge.vue`, `TicketStatusBadge.vue`, `StatusTimeline.vue`, `Money.vue`, `ConfirmDialog.vue`, `ToastHost.vue`, `AppNavbar.vue` (GUEST/CUSTOMER: Katalog, Bantuan, Keranjang, Pesanan, Tiket; ADMIN_SISTEM: Admin, Knowledge; ADMIN_DEV: Dev).

## 5. Struktur Proyek
```
service-frontend/
├── nuxt.config.ts          # ssr:false, runtimeConfig.public.apiBase, devServer.port=3000, tailwind
├── app.vue  layouts/default.vue
├── middleware/auth.global.ts
├── composables/{useAuth,useApi,useChat,useCart}.ts
├── components/...
├── pages/
│   ├── index.vue  help.vue  cart.vue  checkout.vue
│   ├── products/[id].vue
│   ├── orders/{index,[id]}.vue
│   ├── tickets/{index,new,[id]}.vue
│   ├── admin/{index,products,orders,tickets,knowledge}.vue
│   └── dev/index.vue
└── types/api.ts            # lihat §6
```

## 6. Kontrak Tipe (`types/api.ts`) — cermin backend Go
```ts
export interface Product { id: string; name: string; description: string; specifications: string; category: string
  price: number; stock: number; stock_status: 'IN_STOCK'|'LOW_STOCK'|'OUT_OF_STOCK'; image_url: string|null
  is_active: boolean; created_at: string; updated_at: string; score?: number|null }          // score hanya pada semantic-search
export interface KnowledgeChunk { chunk_id: string; document_id: string; title: string
  category: 'RETURN'|'WARRANTY'|'SHIPPING'|'PAYMENT'|'ORDER'|'COMPLAINT'|'GENERAL'; content: string; score: number|null }
export interface KnowledgeDocument { id: string; title: string; category: KnowledgeChunk['category']; content?: string
  is_active: boolean; chunk_count: number; created_at: string; updated_at: string }
export interface OrderItem { product_id: string; product_name: string; unit_price: number; quantity: number; subtotal: number }
export interface Order { id: string; user_id: string
  status: 'PENDING_PAYMENT'|'PAID'|'PROCESSING'|'SHIPPED'|'DELIVERED'|'CANCELLED'|'PAYMENT_FAILED'|'PAYMENT_TIMEOUT'
  payment_status: 'UNPAID'|'PAID'|'FAILED'|'EXPIRED'|'REFUNDED'; payment_method: 'EWALLET'|'BANK_TRANSFER'|'CREDIT_CARD'
  total_amount: number; shipping_address: string; tracking_number: string|null; failure_reason: string|null
  payment_expires_at: string|null; paid_at: string|null; items: OrderItem[]
  history?: { from_status: string|null; to_status: string; note: string|null; actor_id: string; created_at: string }[]
  created_at: string; updated_at: string }
export interface Ticket { id: string; user_id: string; order_id: string|null; subject: string; description: string
  category: 'PAYMENT'|'SHIPPING'|'PRODUCT'|'TECHNICAL'|'OTHER'; priority: 'LOW'|'MEDIUM'|'HIGH'
  status: 'OPEN'|'IN_PROGRESS'|'RESOLVED'|'CLOSED'; assigned_to: string|null; resolution_note: string|null
  created_at: string; updated_at: string; resolved_at: string|null; closed_at: string|null
  messages?: { id: number; author_id: string; author_role: string; message: string; created_at: string }[] }
export interface ApiMeta { total?: number; page?: number; limit?: number; mode?: 'semantic'|'keyword_fallback'; query?: string; min_score?: number }
```
Perubahan skema backend **wajib** disertai pembaruan file ini (satu-satunya sumber tipe frontend).

## 7. Konfigurasi
```ts
// nuxt.config.ts (inti)
export default defineNuxtConfig({
  ssr: false,                                                         // SPA murni — wajib
  devServer: { port: 3000 },
  runtimeConfig: { public: { apiBase: 'http://localhost:8080' } },    // override: NUXT_PUBLIC_API_BASE
  modules: ['@nuxtjs/tailwindcss'],
})
```
- Tanpa `routeRules` SSR per-rute dan tanpa `server/` (Nitro) — semua panggilan API langsung ke Gateway.
- Browser memanggil `http://localhost:8080` lintas-origin: `Authorization` memicu preflight `OPTIONS`; Gateway wajib menjawab `204` dengan `Access-Control-Allow-Origin: http://localhost:3000` (`CORS_ORIGIN`), `Allow-Headers: Authorization, Content-Type, x-request-id`.
- Build statis (`nuxt generate`) menghasilkan SPA yang dapat disajikan server statis mana pun.

## 8. Aksesibilitas & UX
- Responsif (≥360 px), kontras WCAG AA, fokus terlihat.
- Skeleton loading; empty state informatif (khususnya hasil pencarian semantik/kebijakan kosong).
- `ConfirmDialog` untuk batal pesanan, tutup tiket, nonaktifkan produk/dokumen.

## 9. Kriteria Penerimaan
- [ ] `nuxt.config.ts` memuat `ssr: false`; `nuxt build` tidak menghasilkan server bundle yang dibutuhkan runtime.
- [ ] Token JWT tersimpan di `localStorage['toko_token']`; refresh halaman tetap login (via `/auth/me`); logout menghapusnya.
- [ ] Tanpa login: katalog, pencarian AI, `/help`, dan chat (katalog + kebijakan) berfungsi; `/orders` → redirect.
- [ ] Klik "Budi (CUSTOMER)" di RoleSwitcher → login tanpa mengetik apa pun.
- [ ] Ganti ke Siti → pesanan Budi tidak terlihat; chat di-reset.
- [ ] Toggle "Pencarian AI" + "mouse yang tidak bikin pergelangan pegal" → `PRD-014` teratas; saat Embedding API mati → banner `keyword_fallback`.
- [ ] `/help` + "syarat retur barang rusak" → kartu `KB-001`; kueri tak relevan → empty state.
- [ ] `ADMIN_SISTEM` melihat `/admin/*` termasuk `/admin/knowledge`; `CUSTOMER` ke `/admin` → ditolak; `ADMIN_DEV` ke `/dev` ok.
- [ ] Alur: produk → keranjang → checkout → bayar → `PAID`.
- [ ] Chat: "Kenapa ORD-003 gagal?" menampilkan alasan; `ORD-003` menjadi tautan.
- [ ] Semua tipe di `types/api.ts` cocok dengan respons nyata Go (field `snake_case`, `null` eksplisit, envelope `{data}`/`{error}`).
- [ ] Browser tidak pernah memanggil port selain `8080`.
