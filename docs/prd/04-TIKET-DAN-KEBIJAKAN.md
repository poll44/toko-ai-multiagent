# 04 — PRD Tiket Komplain & Pusat Bantuan (Kebijakan Toko)

| Item | Nilai |
|---|---|
| Rujukan | [`00-MASTER-PRD.md`](00-MASTER-PRD.md) |
| Spesifikasi teknis | [`TRD 04 Ticket`](../trd/04-SERVICE-TICKET.md) |

## 1. Tujuan
1. Pelanggan mendapat jawaban atas pertanyaan aturan toko **tanpa perlu menghubungi layanan pelanggan**, dengan jawaban yang bersumber dari dokumen resmi.
2. Bila masalah belum terselesaikan oleh aturan atau status pesanan, pelanggan dapat mengajukan **tiket** dan memantau penanganannya.

## 2. Pusat Bantuan (Kebijakan Toko)

### 2.1 Kebutuhan fungsional
| ID | Kebutuhan | Prioritas |
|---|---|---|
| B-01 | Siapa pun (termasuk Tamu) dapat mencari kebijakan toko dengan kalimat bebas, mis. "syarat pengembalian barang rusak", walau kata "retur" tidak muncul | Wajib |
| B-02 | Hasil berupa kutipan bagian dokumen yang relevan, disertai judul dan kategori dokumen | Wajib |
| B-03 | Pengguna dapat membuka dokumen lengkap dan melihat daftar dokumen per kategori | Wajib |
| B-04 | Pencarian tanpa hasil relevan menampilkan "tidak ada informasi resmi" dan menawarkan membuat tiket — tidak menampilkan hasil asal | Wajib |
| B-05 | Bila layanan AI tidak tersedia, pencarian tetap berjalan dengan kata kunci dan pengguna diberi tahu hasilnya mungkin kurang tepat | Wajib |
| B-06 | Admin Sistem dapat menambah, mengubah, dan menonaktifkan dokumen kebijakan; perubahan langsung dapat ditemukan lewat pencarian | Wajib |
| B-07 | Admin dapat memicu pengindeksan ulang seluruh dokumen | Sebaiknya |
| B-08 | Dokumen nonaktif tidak muncul bagi non-admin | Wajib |

### 2.2 Kategori dokumen
Retur, Garansi, Pengiriman, Pembayaran, Pesanan, Komplain, Umum.

### 2.3 Dokumen kebijakan demo
| ID | Kategori | Judul | Isi pokok |
|---|---|---|---|
| `KB-001` | Retur | Syarat dan Prosedur Retur | Maks. 7 hari sejak diterima; belum dipakai dan kemasan utuh; produk cacat/salah kirim wajib foto/video unboxing; ongkir retur ditanggung toko bila cacat/salah kirim |
| `KB-002` | Garansi | Kebijakan Garansi | 12 bulan untuk kerusakan pabrik; tidak mencakup jatuh, cairan, modifikasi; klaim lewat tiket |
| `KB-003` | Pengiriman | Estimasi Pengiriman | Jabodetabek 1–2 hari kerja, Jawa 2–4, luar Jawa 4–7; resi terbit saat dikirim |
| `KB-004` | Komplain | Prosedur Komplain | Cek status & kebijakan dulu; bila belum terjawab, buat tiket |
| `KB-005` | Pembayaran | Pembayaran Gagal dan Kedaluwarsa | Batas bayar 15 menit; gagal/kedaluwarsa bersifat final; stok kembali; dapat memesan ulang |
| `KB-006` | Pesanan | Pembatalan Pesanan | Pelanggan membatalkan sendiri hanya saat belum dibayar; setelah itu lewat tiket ke admin |
| `KB-007` | Pembayaran | Kebijakan Refund | Ke metode asal, 3–7 hari kerja setelah disetujui |
| `KB-008` | Umum | Jam Layanan dan Kontak | Senin–Jumat 09.00–17.00 WIB; tiket di luar jam diproses hari kerja berikutnya |

> Kebijakan di atas fiktif untuk demo dan harus konsisten dengan aturan pesanan (PRD 03).

## 3. Tiket Komplain

### 3.1 Data tiket
Subjek, deskripsi, kategori (Pembayaran, Pengiriman, Produk, Teknis, Lainnya), prioritas (Rendah, Sedang, Tinggi), nomor pesanan terkait (opsional), penerima tugas, catatan penyelesaian, dan percakapan (pesan bolak-balik).

### 3.2 Kebutuhan fungsional
| ID | Kebutuhan | Prioritas |
|---|---|---|
| T-01 | Pelanggan dapat membuat tiket; nomor pesanan terkait harus **milik pelanggan itu** | Wajib |
| T-02 | Sistem menolak tiket kedua untuk pesanan yang sama bila tiket sebelumnya masih aktif, dan memberi tahu nomor tiket yang ada | Wajib |
| T-03 | Tiket kategori Teknis dari pelanggan berprioritas minimal Sedang | Wajib |
| T-04 | Saat mengisi tiket kategori Pembayaran/Pengiriman/Produk, pengguna melihat saran jawaban dari Pusat Bantuan (tidak memblokir pengiriman tiket) | Sebaiknya |
| T-05 | Pelanggan melihat daftar dan detail tiket miliknya; admin melihat semua | Wajib |
| T-06 | Pelanggan dan admin dapat saling berbalas pesan pada tiket yang belum ditutup | Wajib |
| T-07 | Admin Sistem dapat memproses tiket apa pun; Developer hanya tiket kategori Teknis (dapat membaca semuanya) | Wajib |
| T-08 | Pelanggan dapat **membuka kembali** tiket yang sudah diselesaikan (wajib menyertakan pesan) dan **menutup** tiket | Wajib |
| T-09 | Menyelesaikan tiket mewajibkan catatan penyelesaian (min. 10 karakter) | Wajib |
| T-10 | Setiap perubahan status tercatat dengan pelaku dan waktu | Wajib |

### 3.3 Siklus hidup tiket
```
Terbuka ──mulai ditangani──► Sedang diproses ──selesai──► Selesai ──tutup──► Ditutup (final)
   ▲  └──── kembali ────────────┘                           │
   └──────────── buka kembali (pelanggan/admin) ────────────┘
Terbuka ──tutup langsung (pelanggan pemilik / admin)──► Ditutup
```
| Dari → Ke | Siapa | Syarat |
|---|---|---|
| Terbuka → Sedang diproses | Admin (Developer: teknis) | Penangan tercatat |
| Sedang diproses → Selesai | Admin (Developer: teknis) | Catatan penyelesaian |
| Sedang diproses → Terbuka | Admin | Penangan dilepas |
| Selesai → Terbuka | Pelanggan pemilik, admin | Pesan alasan wajib |
| Selesai → Ditutup | Pelanggan pemilik, admin | — |
| Terbuka → Ditutup | Pelanggan pemilik, Admin Sistem | — |
| Ditutup → apa pun | — | Final |

Perubahan di luar tabel ditolak dengan "perubahan status tidak sah". Pesan ke tiket Ditutup ditolak.

## 4. Aturan Bisnis
1. Pelanggan hanya dapat melihat dan mengubah tiket miliknya; tiket orang lain "tidak ditemukan".
2. Pemilik dan penangan tiket selalu ditentukan sistem, bukan dari input pengguna.
3. Tamu tidak dapat membuat atau melihat tiket, tetapi dapat memakai Pusat Bantuan.
4. Jawaban kebijakan **hanya** dari dokumen; tidak ada dokumen relevan berarti tidak ada jawaban resmi.

## 5. Data Demo Tiket
| Tiket | Pemilik | Pesanan | Kategori | Status | Judul |
|---|---|---|---|---|---|
| `TKT-001` | Budi | `ORD-003` | Pembayaran | Terbuka | Pembayaran ORD-003 gagal padahal saldo cukup |
| `TKT-002` | Budi | `ORD-001` | Produk | Selesai | Pertanyaan garansi earbuds |
| `TKT-003` | Siti | `ORD-005` | Pengiriman | Sedang diproses | Estimasi pengiriman belum jelas |
| `TKT-004` | Siti | — | Teknis | Terbuka | Halaman checkout error saat memilih e-wallet |
| `TKT-005` | Siti | `ORD-008` | Produk | Ditutup | Webcam terbungkus kurang rapi |

## 6. Kriteria Diterima
- [ ] Budi melihat hanya `TKT-001` dan `TKT-002`; membuka `TKT-003` → "tidak ditemukan".
- [ ] Budi membuat tiket untuk `ORD-005` (milik Siti) → ditolak "tidak ditemukan".
- [ ] Tiket kedua untuk `ORD-003` saat `TKT-001` masih aktif → ditolak dan menyebut `TKT-001`.
- [ ] Terbuka → Selesai langsung ditolak; Ditutup tidak dapat diubah.
- [ ] Developer mengubah tiket Pembayaran → ditolak; tiket Teknis (`TKT-004`) → berhasil.
- [ ] Tamu tidak dapat melihat tiket, tetapi dapat mencari kebijakan.
- [ ] Pencarian "berapa lama batas waktu bayar" menempatkan `KB-005` di peringkat 1.
- [ ] Pencarian "syarat pengembalian barang rusak" (tanpa kata "retur") menemukan `KB-001`.
- [ ] Pencarian "resep rendang" tidak menghasilkan hasil.
- [ ] Pelanggan tidak dapat mengelola dokumen kebijakan; dokumen baru dari Admin langsung dapat dicari.
- [ ] Saat layanan AI mati, pencarian kebijakan tetap berjalan dengan peringatan.
