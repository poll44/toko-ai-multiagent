# 02 — PRD Katalog & Pencarian Produk

| Item | Nilai |
|---|---|
| Rujukan | [`00-MASTER-PRD.md`](00-MASTER-PRD.md) |
| Spesifikasi teknis | [`TRD 02 Catalog`](../trd/02-SERVICE-CATALOG.md) |

## 1. Tujuan
Pelanggan menemukan produk yang tepat, baik saat tahu nama/kata kuncinya maupun saat hanya bisa menjelaskan kebutuhannya. Admin menjaga data produk dan stok tetap akurat.

## 2. Informasi Produk
Setiap produk memiliki: nama, deskripsi, spesifikasi, kategori (audio, aksesoris, wearable, komputer, penyimpanan), harga (Rupiah), stok, gambar (opsional), dan status aktif.

**Status stok** yang ditampilkan ke pengguna:

| Status | Aturan | Tampilan |
|---|---|---|
| Tersedia | stok lebih dari 10 | "Tersedia" |
| Stok menipis | stok 1–10 | "Stok menipis" (sebut sisa, mis. "tinggal 8") |
| Habis | stok 0 | "Habis"; tidak bisa dimasukkan ke keranjang atau dipesan |

## 3. Kebutuhan Fungsional

### 3.1 Penelusuran & pencarian biasa
| ID | Kebutuhan | Prioritas |
|---|---|---|
| K-01 | Pengguna dapat melihat daftar produk dengan paginasi | Wajib |
| K-02 | Pengguna dapat mencari berdasarkan kata kunci pada nama, deskripsi, dan spesifikasi | Wajib |
| K-03 | Pengguna dapat memfilter berdasarkan kategori, rentang harga, dan "hanya yang tersedia" | Wajib |
| K-04 | Pengguna dapat mengurutkan berdasarkan nama, harga (naik/turun), atau terbaru | Wajib |
| K-05 | Pengguna dapat melihat daftar kategori beserta jumlah produknya | Wajib |
| K-06 | Pengguna dapat membuka detail produk lengkap dengan spesifikasi | Wajib |
| K-07 | Produk nonaktif tidak tampil bagi Tamu dan Pelanggan | Wajib |

### 3.2 Pencarian AI (semantik)
| ID | Kebutuhan | Prioritas |
|---|---|---|
| S-01 | Pengguna dapat mengetik kebutuhan, keluhan, atau skenario pemakaian dalam kalimat bebas, dan sistem mengembalikan produk yang **maknanya cocok**, walau kata kuncinya tidak persis sama | Wajib |
| S-02 | Hasil diurutkan dari yang paling cocok; hanya produk yang cukup relevan yang ditampilkan | Wajib |
| S-03 | Pencarian AI dapat dikombinasikan dengan filter kategori, harga, dan ketersediaan | Wajib |
| S-04 | Tiap hasil menampilkan tingkat kecocokan dalam bentuk label ("Sangat cocok" / "Cocok"), **bukan angka mentah** | Wajib |
| S-05 | Harga dan stok pada hasil selalu data terbaru | Wajib |
| S-06 | Bila tidak ada produk yang cukup cocok, tampilkan hasil kosong yang informatif — bukan produk acak | Wajib |
| S-07 | Kalimat yang tidak relevan dengan toko (mis. "resep rendang") tidak menghasilkan produk | Wajib |
| S-08 | Bila layanan AI tidak tersedia, pencarian tetap berjalan dengan kata kunci dan pengguna diberi tahu bahwa **pencarian cerdas sedang terbatas** | Wajib |
| S-09 | Produk baru atau yang diubah deskripsinya dapat ditemukan lewat pencarian AI tanpa tindakan manual admin | Wajib |
| S-10 | Perubahan yang hanya menyangkut harga/stok/gambar/status tidak mengubah hasil pencarian makna | Sebaiknya |

**Contoh perilaku yang diharapkan**

| Kalimat pengguna | Hasil |
|---|---|
| "mouse yang tidak bikin pergelangan pegal" | Mouse Vertikal Ergonomis (`PRD-014`) di peringkat 1, di atas mouse biasa (`PRD-008`) |
| "powerbank di bawah 400 ribu" (pencarian biasa + filter) | Powerbank 20000mAh (`PRD-003`) |
| "resep rendang" | Tidak ada hasil |

### 3.3 Pengelolaan produk (Admin Sistem)
| ID | Kebutuhan | Prioritas |
|---|---|---|
| M-01 | Admin dapat menambah produk (nama, deskripsi, spesifikasi, kategori, harga, stok awal, gambar) | Wajib |
| M-02 | Admin dapat mengubah data produk dan mengaktifkan/menonaktifkan produk | Wajib |
| M-03 | Menghapus produk berarti **menonaktifkan** (data dan riwayat tetap ada) | Wajib |
| M-04 | Admin dapat mengatur stok: mengisi ulang ke angka tertentu atau menambah/mengurangi; stok tidak boleh negatif | Wajib |
| M-05 | Setiap perubahan stok tercatat (jumlah, alasan, siapa, kapan) dan dapat dilihat Admin dan Developer | Wajib |
| M-06 | Admin dapat memicu pengindeksan ulang pencarian AI untuk semua produk | Sebaiknya |
| M-07 | Input divalidasi dan semua kesalahan ditampilkan sekaligus (nama 3–120 karakter, harga/stok bilangan bulat ≥ 0, dst.) | Wajib |

## 4. Aturan Bisnis
1. **Stok berkurang saat pesanan dibuat**, bukan saat dibayar, agar barang tidak terjual ganda.
2. **Stok kembali** saat pesanan gagal bayar, kedaluwarsa, atau dibatalkan; pengembalian hanya terjadi sekali per pesanan.
3. Pengurangan stok untuk satu pesanan berlaku **semua-atau-tidak-sama-sekali**: bila satu barang kurang, tidak ada stok yang berubah.
4. Harga dan nama pada pesanan diambil dari katalog pada saat pesanan dibuat (harga yang dikirim klien diabaikan).
5. Produk nonaktif tidak dapat dipesan.

## 5. Kriteria Diterima
- [ ] "powerbank" dengan batas harga 400 ribu hanya menghasilkan `PRD-003`.
- [ ] Pencarian AI "mouse yang tidak bikin pergelangan pegal" menempatkan `PRD-014` di peringkat 1.
- [ ] Pencarian AI "resep rendang" tidak menghasilkan produk.
- [ ] Kalimat pencarian terlalu pendek (kurang dari 3 karakter) ditolak dengan pesan yang jelas.
- [ ] Saat layanan AI mati, pencarian tetap bekerja dan pengguna melihat peringatan pencarian terbatas.
- [ ] Produk baru langsung dapat ditemukan lewat pencarian AI dalam waktu singkat (maksimal satu siklus pemrosesan ulang otomatis bila gagal seketika).
- [ ] Mengubah hanya harga tidak memicu pengindeksan ulang.
- [ ] Pelanggan tidak dapat menambah/mengubah produk; produk nonaktif (`PRD-013`) tidak muncul untuk non-admin di kedua jenis pencarian.
- [ ] Stok habis (`PRD-010`) tidak bisa dipesan; stok menipis (`PRD-012`) menampilkan sisa.
- [ ] Pemesanan dengan satu barang kurang stok tidak mengubah stok barang lain; pengembalian stok dua kali hanya menambah sekali.
