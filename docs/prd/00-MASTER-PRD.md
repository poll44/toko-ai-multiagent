# 00 — MASTER PRD: Toko AI

| Item | Nilai |
|---|---|
| Versi dokumen | 3.0 (PRD dipisah dari TRD) |
| Status | Siap implementasi |
| Pembaca | Product owner, desainer, QA, developer |
| Dokumen teknis | [`docs/trd/`](../trd/00-MASTER-TRD.md) — arsitektur, kontrak API, skema data, stack |

> **Batas dokumen.** PRD menjawab **apa** yang dibutuhkan pengguna dan **mengapa**: masalah, target, perilaku produk, aturan bisnis, kriteria diterima. PRD **tidak** memuat nama library, port, skema tabel, path endpoint, header HTTP, atau potongan kode. Semua itu ada di TRD.

### Daftar dokumen PRD
| File | Cakupan |
|---|---|
| `00-MASTER-PRD.md` | Visi, pengguna, ruang lingkup, perjalanan pengguna, metrik (dokumen ini) |
| `01-AKUN-DAN-AKSES.md` | Peran, masuk ke aplikasi, hak akses |
| `02-KATALOG-DAN-PENCARIAN.md` | Produk, stok, pencarian biasa dan pencarian AI |
| `03-PESANAN-DAN-PEMBAYARAN.md` | Pemesanan, pembayaran (simulasi), status pesanan |
| `04-TIKET-DAN-KEBIJAKAN.md` | Tiket komplain, pusat bantuan (kebijakan toko) |
| `05-ASISTEN-AI.md` | Perilaku asisten belanja & layanan pelanggan |
| `06-PENGALAMAN-PENGGUNA.md` | Halaman, alur layar, aturan tampilan |

---

## 1. Latar Belakang & Masalah

Toko elektronik & aksesoris online biasanya membuat pelanggan bekerja sendiri: menebak kata kunci yang tepat, membaca halaman kebijakan yang panjang, dan membuat tiket komplain untuk hal yang sebenarnya sudah dijelaskan di kebijakan.

| Masalah pelanggan | Dampak |
|---|---|
| Pencarian kata kunci gagal untuk kebutuhan yang diungkapkan bebas ("mouse yang tidak bikin pergelangan pegal") | Pelanggan tidak menemukan produk yang sebenarnya ada |
| Aturan toko (retur, garansi, pengiriman) tersebar dan sulit dicari | Pertanyaan berulang ke layanan pelanggan |
| Pesanan gagal tanpa penjelasan yang jelas | Frustrasi, tiket komplain yang sebenarnya tidak perlu |
| Asisten AI yang mengarang jawaban | Janji palsu, hilangnya kepercayaan |

## 2. Visi

**Toko AI** adalah toko elektronik & aksesoris dengan **asisten belanja dan layanan pelanggan berbasis AI** yang bisa dipercaya: membantu menemukan produk dari bahasa sehari-hari, menjawab pertanyaan kebijakan dengan kutipan resmi, mengurus pesanan, dan membuat tiket komplain lewat percakapan — tanpa pernah mengarang data toko.

## 3. Tujuan & Non-Tujuan

### Tujuan
1. Pelanggan dapat **menemukan produk** lewat kata kunci, filter, atau kalimat bebas.
2. Pelanggan dapat **bertanya kebijakan toko** dan mendapat jawaban yang bersumber dari dokumen resmi.
3. Pelanggan dapat **memesan, membayar (simulasi), memantau, dan membatalkan** pesanan, baik lewat antarmuka biasa maupun lewat percakapan.
4. Pelanggan dapat **mengajukan dan memantau tiket komplain**, dengan asisten yang lebih dulu memeriksa kebijakan dan status pesanan.
5. Setiap pengguna **hanya melihat dan mengubah data miliknya**; admin punya kewenangan sesuai perannya.
6. Asisten AI **jujur**: fakta toko hanya dari data sistem; bila tidak tahu, ia menyatakannya.

### Non-Tujuan
- Pembayaran dan pengiriman nyata, multi-toko (multi-tenant), skala produksi.
- Keamanan akun tingkat produksi (kata sandi demo `123456` hanya untuk demonstrasi).
- Rekomendasi personal berbasis riwayat belanja, promosi/voucher, ulasan produk.

## 4. Pengguna (Persona)

| Persona | Peran | Kebutuhan utama |
|---|---|---|
| **Tamu** | Pengunjung tanpa akun | Melihat katalog, mencari produk (termasuk pencarian AI), membaca kebijakan, bertanya ke asisten tentang produk dan kebijakan |
| **Budi / Siti** | Pelanggan (`CUSTOMER`) | Memesan, membayar, memantau pesanan, mengajukan komplain, bertanya ke asisten tentang data pribadinya |
| **Admin Sistem** | Operasional toko (`ADMIN_SISTEM`) | Mengelola produk & stok, memproses pesanan, menangani tiket, mengelola dokumen kebijakan |
| **Developer** | Teknisi (`ADMIN_DEV`) | Memantau kesehatan sistem, menangani tiket berkategori teknis, membaca data (tanpa mengubah) |

## 5. Ruang Lingkup Fitur

| # | Kapabilitas | Tamu | Pelanggan | Admin Sistem | Developer | PRD |
|---|---|:-:|:-:|:-:|:-:|---|
| F1 | Masuk cepat dengan akun demo | ✅ | ✅ | ✅ | ✅ | 01 |
| F2 | Melihat & mencari produk (kata kunci, filter) | ✅ | ✅ | ✅ | ✅ | 02 |
| F3 | Pencarian AI (kalimat bebas) | ✅ | ✅ | ✅ | ✅ | 02 |
| F4 | Mengelola produk & stok | — | — | ✅ | 👁 | 02 |
| F5 | Membuat, membayar, membatalkan pesanan | — | ✅ | batalkan | — | 03 |
| F6 | Memproses pesanan (pengiriman) | — | — | ✅ | — | 03 |
| F7 | Pusat Bantuan (cari kebijakan toko) | ✅ | ✅ | ✅ | ✅ | 04 |
| F8 | Mengelola dokumen kebijakan | — | — | ✅ | — | 04 |
| F9 | Tiket komplain (ajukan, balas, pantau) | — | ✅ | ✅ | teknis | 04 |
| F10 | Asisten AI (belanja & layanan pelanggan) | produk & kebijakan | semua fitur milik sendiri | semua | semua | 05 |
| F11 | Dashboard admin & developer | — | — | ✅ | ✅ | 06 |

Legenda: ✅ penuh · 👁 hanya baca · "teknis" = hanya tiket kategori teknis.

## 6. Perjalanan Pengguna Utama

### J1 — Belanja dengan bantuan asisten (Pelanggan)
1. Budi masuk lewat pemilih akun demo.
2. Bertanya: *"Cari powerbank di bawah 400 ribu."* Asisten menampilkan produk yang cocok beserta harga dan stok.
3. Budi: *"Pesan 1 ya."* Asisten menampilkan **ringkasan pesanan** (barang, jumlah, harga, total, metode bayar, alamat) dan meminta konfirmasi.
4. Budi menyetujui. Pesanan dibuat; asisten menyebut nomor pesanan dan **batas waktu bayar 15 menit**.
5. Budi membayar di halaman pesanan; status menjadi **Dibayar**.

### J2 — Pesanan gagal dan komplain (Pelanggan)
1. Budi: *"Pesanan ORD-003 kok belum jalan?"*
2. Asisten memeriksa pesanan, menjelaskan alasan gagal persis seperti tercatat, lalu menjelaskan kebijakan pembayaran gagal dari dokumen resmi (pesanan final, stok dikembalikan, bisa pesan ulang).
3. Budi: *"Saldo saya cukup tapi tetap gagal, buatkan tiket."*
4. Asisten memeriksa apakah sudah ada tiket aktif untuk pesanan itu. Bila ada, memberi tahu nomornya (tanpa duplikat). Bila belum, meminta konfirmasi lalu membuat tiket.
5. Admin memproses tiket; Budi memantau statusnya, dari asisten maupun dari halaman tiket.

### J3 — Menemukan produk dari kebutuhan bebas (Tamu/Pelanggan)
1. Siti: *"Aku butuh mouse yang tidak bikin pergelangan tangan pegal."*
2. Sistem mencari berdasarkan **makna**, bukan kata persis, dan menempatkan **Mouse Vertikal Ergonomis** di urutan teratas.
3. Asisten merekomendasikan dengan alasan yang dikutip dari deskripsi produk, setelah memastikan stok dan harga terbaru.

### J4 — Bertanya kebijakan sebelum bertindak (Pelanggan)
1. Budi: *"Barang saya rusak, bisa dikembalikan? Berapa lama?"*
2. Asisten mencari di dokumen kebijakan dan menjawab sesuai isinya (mis. retur ≤ 7 hari, wajib video unboxing) sambil menyebut judul dokumen sumber.
3. Hanya jika Budi ingin melanjutkan, asisten menyiapkan tiket dan meminta konfirmasi.

### J5 — Pertanyaan di luar pengetahuan toko
*"Apakah ada cicilan 0%?"* → tidak ada di dokumen kebijakan → asisten menjawab bahwa **belum ada informasi resmi** dan menawarkan tiket. Asisten tidak menebak.

## 7. Prinsip Produk (berlaku di semua fitur)

| # | Prinsip | Artinya bagi pengguna |
|---|---|---|
| P1 | **Jujur, tidak mengarang** | Produk, harga, stok, status pesanan/tiket, dan kebijakan hanya berasal dari data sistem |
| P2 | **Data milik sendiri** | Pelanggan tidak bisa melihat pesanan/tiket orang lain; mencobanya hasilnya "tidak ditemukan" |
| P3 | **Konfirmasi sebelum tindakan** | Membuat pesanan, membayar, membatalkan, dan membuat tiket lewat asisten selalu didahului ringkasan dan persetujuan eksplisit |
| P4 | **Kebijakan dijawab dari dokumen** | Jawaban kebijakan mengutip dokumen resmi; kosong berarti "tidak ada informasi" |
| P5 | **Tetap berguna saat AI terbatas** | Jika pencarian cerdas tidak tersedia, pencarian kata kunci tetap bekerja dan pengguna diberi tahu |
| P6 | **Bahasa Indonesia** | Seluruh antarmuka, pesan galat, dan jawaban asisten berbahasa Indonesia; uang dalam Rupiah (`Rp299.000`) |

## 8. Metrik Keberhasilan

| Metrik | Target |
|---|---|
| Semua bagian sistem berjalan | 7 dari 7 layanan hidup |
| Kebocoran data antar pelanggan | 0 kasus |
| Pencarian AI "mouse yang tidak bikin pergelangan pegal" | Mouse Vertikal Ergonomis (`PRD-014`) peringkat 1 |
| Jawaban kebijakan tanpa merujuk dokumen resmi | 0 kasus |
| Asisten menyebut data pesanan/stok tanpa memeriksa sistem | 0 kasus |
| Tiket duplikat untuk pesanan yang sama saat masih aktif | 0 kasus lewat asisten |
| Tindakan tulis lewat asisten tanpa persetujuan pengguna | 0 kasus |

## 9. Data Demo (untuk pengujian skenario)

| Kebutuhan uji | Data |
|---|---|
| Akun | `budi@toko.com`, `siti@toko.com` (pelanggan); `admin@toko.com`; `dev@toko.com` — semua memakai kata sandi demo |
| Produk normal, murah, stok habis (`PRD-010`), stok menipis (`PRD-012`), nonaktif (`PRD-013`) | 14 produk |
| Pesanan gagal bayar (`ORD-003`), kedaluwarsa (`ORD-004`), milik pelanggan lain (`ORD-005`), menunggu bayar (`ORD-006`) | 8 pesanan |
| Tiket berbagai status & kategori | 5 tiket |
| Dokumen kebijakan: retur, garansi, pengiriman, komplain, pembayaran gagal, pembatalan, refund, jam layanan | 8 dokumen (`KB-001`…`KB-008`) |

Rincian data seed: lihat TRD per layanan.

## 10. Asumsi, Risiko, Pertanyaan Terbuka

| Jenis | Isi |
|---|---|
| Asumsi | Pembayaran hanya simulasi (berhasil / gagal / kedaluwarsa); kebijakan toko bersifat fiktif untuk demo |
| Risiko | Layanan AI eksternal bisa lambat atau tidak tersedia → ada perilaku cadangan (P5) |
| Risiko | Model AI dapat salah menafsirkan → dibatasi aturan jujur (P1–P4) dan guard di sisi sistem (lihat TRD agent) |
| Di luar lingkup v1 | Skala besar (ribuan produk), pembayaran nyata, notifikasi email/WhatsApp |
