# 01 — PRD Akun & Hak Akses

| Item | Nilai |
|---|---|
| Rujukan | [`00-MASTER-PRD.md`](00-MASTER-PRD.md) |
| Spesifikasi teknis | [`TRD 01 Gateway`](../trd/01-SERVICE-GATEWAY.md), [`TRD 00 §5–6`](../trd/00-MASTER-TRD.md) |

## 1. Tujuan
Pengguna dapat berganti peran dengan cepat untuk mencoba seluruh skenario, dan setiap peran hanya dapat melakukan hal yang menjadi haknya.

## 2. Peran

| Peran | Siapa | Ringkasan hak |
|---|---|---|
| Tamu | Belum masuk | Lihat katalog, cari produk, baca kebijakan, chat dengan asisten (produk & kebijakan saja) |
| Pelanggan | Budi, Siti | Semua hak tamu + pesanan dan tiket **milik sendiri** |
| Admin Sistem | Admin | Mengelola produk, stok, pesanan, tiket, dan dokumen kebijakan |
| Developer | Dev | Membaca semua data; menangani tiket kategori teknis; melihat kesehatan sistem |

## 3. Kebutuhan Fungsional

| ID | Kebutuhan | Prioritas |
|---|---|---|
| A-01 | Pengguna dapat masuk dengan **satu klik** pada daftar akun demo, tanpa mengetik email atau kata sandi | Wajib |
| A-02 | Daftar akun demo menampilkan nama dan peran, dikelompokkan per peran; kata sandi tidak pernah ditampilkan | Wajib |
| A-03 | Pengguna dapat keluar dan kembali menjadi Tamu | Wajib |
| A-04 | Sesi bertahan saat halaman dimuat ulang, dan berakhir otomatis setelah 8 jam | Wajib |
| A-05 | Saat sesi berakhir, pengguna diberi tahu ("Sesi berakhir") dan kembali menjadi Tamu | Wajib |
| A-06 | Mengakses halaman yang bukan haknya mengarahkan pengguna ke beranda dengan pesan "Akses ditolak" | Wajib |
| A-07 | Mengakses halaman yang butuh login sebagai Tamu membuka pemilih akun dan, setelah masuk, melanjutkan ke halaman tujuan | Wajib |
| A-08 | Setelah masuk, pengguna diarahkan ke halaman awal sesuai perannya (Pelanggan → beranda, Admin → dashboard admin, Developer → dashboard developer) | Wajib |
| A-09 | Percobaan masuk (berhasil/gagal) tercatat untuk audit | Sebaiknya |

## 4. Aturan Bisnis
1. Akun nonaktif tidak dapat masuk; pesan galat sama dengan kata sandi salah (tidak membocorkan keberadaan akun).
2. Pelanggan yang mengakses pesanan/tiket milik pelanggan lain mendapat hasil **"tidak ditemukan"**, bukan "dilarang", agar keberadaan data tidak terungkap.
3. Pelanggaran peran (mis. pelanggan mencoba mengubah produk) ditolak dengan pesan "tidak berhak".
4. Pengguna tidak dapat mengaku sebagai peran lain, baik lewat antarmuka maupun lewat percakapan dengan asisten.

## 5. Matriks Hak Akses

Legenda: ✅ boleh · 👁 hanya milik sendiri · 📖 baca semua · ❌ ditolak.

| Kemampuan | Tamu | Pelanggan | Admin Sistem | Developer |
|---|:-:|:-:|:-:|:-:|
| Lihat & cari produk (biasa & AI) | ✅ | ✅ | ✅ | ✅ |
| Lihat produk nonaktif | ❌ | ❌ | ✅ | ✅ |
| Tambah/ubah/nonaktifkan produk, atur stok | ❌ | ❌ | ✅ | ❌ |
| Buat pesanan | ❌ | ✅ | ❌ | ❌ |
| Lihat pesanan | ❌ | 👁 | 📖 | 📖 |
| Bayar pesanan | ❌ | 👁 | ❌ | ❌ |
| Batalkan pesanan | ❌ | 👁 (hanya belum dibayar) | ✅ | ❌ |
| Ubah status pengiriman | ❌ | ❌ | ✅ | ❌ |
| Cari & baca kebijakan toko | ✅ | ✅ | ✅ | ✅ |
| Kelola dokumen kebijakan | ❌ | ❌ | ✅ | ❌ |
| Buat tiket | ❌ | ✅ | ❌ | ❌ |
| Lihat tiket | ❌ | 👁 | 📖 | 📖 |
| Balas / ubah status tiket | ❌ | 👁 (balas, buka kembali, tutup) | ✅ | ✅ (hanya kategori teknis) |
| Chat dengan asisten | ✅ (produk & kebijakan) | ✅ | ✅ | ✅ |
| Lihat kesehatan sistem | ❌ | ❌ | ❌ | ✅ |

## 6. Kriteria Diterima
- [ ] Klik "Budi (Pelanggan)" masuk tanpa mengetik apa pun, dan sesi tetap ada setelah muat ulang.
- [ ] Daftar akun demo tidak memuat kata sandi.
- [ ] Tamu membuka halaman pesanan → diarahkan masuk; Tamu tetap dapat memakai katalog, Pusat Bantuan, dan chat.
- [ ] Pelanggan membuka pesanan milik pelanggan lain → "tidak ditemukan".
- [ ] Pelanggan membuka halaman admin → "Akses ditolak".
- [ ] Mengaku sebagai admin dalam chat tidak mengubah hak akses.
- [ ] Hanya Developer yang dapat membuka halaman kesehatan sistem.
