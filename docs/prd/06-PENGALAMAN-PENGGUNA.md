# 06 — PRD Pengalaman Pengguna (Halaman & Alur Layar)

| Item | Nilai |
|---|---|
| Rujukan | [`00-MASTER-PRD.md`](00-MASTER-PRD.md) |
| Spesifikasi teknis | [`TRD 07 Frontend`](../trd/07-SERVICE-FRONTEND.md) |

## 1. Prinsip Tampilan
1. Tidak ada formulir login; masuk lewat **pemilih akun demo** (satu klik), berlabel "Mode demo — akun contoh, tanpa kata sandi".
2. Uang tampil `Rp299.000`; waktu tampil dalam zona waktu Jakarta.
3. Pesan galat ditampilkan dalam Bahasa Indonesia, jelas, dan tidak teknis; kode referensi permintaan tampil kecil untuk keperluan bantuan.
4. Pembatalan, penutupan tiket, dan penonaktifan produk/dokumen selalu meminta konfirmasi.
5. Responsif (layar ≥ 360 px), kontras memadai, fokus keyboard terlihat, ada tampilan memuat dan tampilan kosong yang informatif.
6. Pembatasan halaman di antarmuka hanya kenyamanan; keamanan ditegakkan di sistem.

## 2. Peta Halaman

| Halaman | Isi | Tamu | Pelanggan | Admin | Dev |
|---|---|:-:|:-:|:-:|:-:|
| Beranda / Katalog | Grid produk, pencarian, filter, urutan, pencarian AI | ✅ | ✅ | ✅ | ✅ |
| Detail produk | Deskripsi, spesifikasi, stok, tombol keranjang, "Tanya Asisten AI tentang produk ini" | ✅ | ✅ (+keranjang) | ✅ | ✅ |
| Pusat Bantuan | Cari & baca kebijakan toko | ✅ | ✅ | ✅ | ✅ |
| Keranjang | Atur jumlah, hapus, total perkiraan | ✅ | ✅ | — | — |
| Checkout | Alamat & metode bayar | prompt masuk | ✅ | — | — |
| Pesanan saya / detail | Daftar, status, riwayat, bayar, batalkan | ❌ | ✅ | → admin | → dev |
| Tiket saya / buat / detail | Daftar, formulir, percakapan | ❌ | ✅ | → admin | → dev |
| Admin: Dashboard | Ringkasan | ❌ | ❌ | ✅ | ❌ |
| Admin: Produk | Kelola produk, stok, pengindeksan ulang | ❌ | ❌ | ✅ | baca |
| Admin: Pesanan | Semua pesanan, proses pengiriman | ❌ | ❌ | ✅ | baca |
| Admin: Tiket | Semua tiket, ubah status | ❌ | ❌ | ✅ | baca (tulis hanya teknis) |
| Admin: Knowledge Base | Kelola dokumen kebijakan, pengindeksan ulang | ❌ | ❌ | ✅ | ❌ |
| Developer: Dashboard | Kesehatan layanan, tiket teknis, identitas sesi | ❌ | ❌ | ❌ | ✅ |

Navigasi: Tamu/Pelanggan — Katalog, Bantuan, Keranjang, Pesanan, Tiket; Admin — Admin, Knowledge; Developer — Dev.

## 3. Perilaku Halaman Utama

### 3.1 Beranda
| ID | Kebutuhan |
|---|---|
| U-01 | Menampilkan grid produk dengan lencana status stok (Habis / Stok menipis / Tersedia); tombol keranjang nonaktif bila stok 0 |
| U-02 | Pencarian kata kunci, filter kategori/harga/tersedia, urutan, paginasi |
| U-03 | **Saklar "Pencarian AI"**: aktif → kotak berubah menjadi kalimat bebas; hasil menampilkan lencana "Sangat cocok" / "Cocok" (tanpa angka) |
| U-04 | Peringatan "Pencarian cerdas sedang terbatas" saat layanan AI tidak tersedia; tampilan kosong "Belum ada produk yang cocok" |

### 3.2 Pusat Bantuan
| ID | Kebutuhan |
|---|---|
| U-05 | Kotak pencarian dengan saran cepat ("syarat retur", "garansi", "estimasi pengiriman", "pembayaran gagal") |
| U-06 | Hasil berupa kartu kutipan (judul dokumen, kategori, isi) dengan tautan "Lihat dokumen lengkap", "Tanya Asisten AI", dan "Buat tiket" (perlu masuk) |
| U-07 | Daftar dokumen per kategori; hasil kosong → "Tidak ada informasi resmi; Anda dapat membuat tiket" |

### 3.3 Keranjang, Checkout, Pesanan
| ID | Kebutuhan |
|---|---|
| U-08 | Keranjang tersimpan di perangkat dan menyatakan bahwa harga final ditetapkan sistem |
| U-09 | Checkout memvalidasi alamat (10–300 karakter) dan metode bayar; galat stok menampilkan barang, stok tersedia, dan jumlah diminta |
| U-10 | Detail pesanan: status, linimasa riwayat, item, alasan gagal, hitung mundur batas bayar, tombol **Bayar**, panel demo **Simulasi gagal / kedaluwarsa**, **Batalkan** (hanya sebelum dibayar), **Buat tiket** |
| U-11 | Pesanan gagal/kedaluwarsa menampilkan tautan ke kebijakan pembayaran gagal |

### 3.4 Tiket
| ID | Kebutuhan |
|---|---|
| U-12 | Formulir tiket: subjek, deskripsi, kategori, prioritas, pesanan terkait (pilihan dari pesanan miliknya); menampilkan saran dari Pusat Bantuan sebelum kirim |
| U-13 | Detail tiket: informasi, percakapan, balasan, tombol Buka kembali / Tutup |

### 3.5 Admin & Developer
| ID | Kebutuhan |
|---|---|
| U-14 | Kelola produk: tabel + formulir (nama, deskripsi, spesifikasi, kategori, harga, stok, gambar), atur stok, nonaktifkan, pengindeksan ulang |
| U-15 | Kelola knowledge base: tabel dokumen (judul, kategori, aktif, jumlah potongan), editor isi, nonaktifkan, pengindeksan ulang |
| U-16 | Admin pesanan/tiket: ubah status dengan catatan (resi, catatan penyelesaian) |
| U-17 | Dashboard developer: status tiap layanan (hidup/mati), tiket teknis, informasi identitas sesi |

## 4. Jendela Chat Asisten AI

| ID | Kebutuhan |
|---|---|
| C-01 | Tombol mengambang kanan-bawah yang membuka panel chat (layar penuh di ponsel); dapat juga disematkan dalam halaman |
| C-02 | Header "Asisten Toko AI" dengan keterangan identitas aktif; gelembung pesan, indikator mengetik, Enter kirim, Shift+Enter baris baru, tombol "Percakapan baru" |
| C-03 | Saran cepat sesuai peran. Tamu: "Cari powerbank di bawah 400 ribu", "Mouse yang tidak bikin pergelangan pegal", "Syarat retur barang?". Pelanggan: "Status pesanan saya", "Kenapa pesanan ORD-003 gagal?", "Berapa lama garansi?", "Buat tiket komplain" |
| C-04 | Bila asisten meminta masuk, tampil tombol "Pilih akun demo" |
| C-05 | Nomor pesanan, tiket, produk, dan dokumen dalam jawaban menjadi tautan ke halaman terkait |
| C-06 | Pesan maksimal 1000 karakter; tombol kirim nonaktif saat menunggu; percakapan tersimpan per pengguna selama sesi peramban |
| C-07 | Teks asisten ditampilkan aman (tanpa menjalankan konten HTML dari jawaban) |
| C-08 | Tombol "Tanya Asisten AI" di halaman produk/bantuan membuka chat dengan pertanyaan awal yang relevan |

## 5. Kriteria Diterima
- [ ] Klik akun demo masuk tanpa mengetik; muat ulang halaman tetap masuk; keluar kembali menjadi Tamu.
- [ ] Tanpa masuk: katalog, pencarian AI, Pusat Bantuan, dan chat (produk & kebijakan) berfungsi; halaman pesanan mengarahkan ke pilihan akun.
- [ ] Saklar Pencarian AI menampilkan lencana kecocokan tanpa angka; peringatan tampil saat pencarian terbatas.
- [ ] Alur checkout → detail pesanan menampilkan hitung mundur; galat stok menjelaskan barang yang kurang.
- [ ] Pesanan gagal menampilkan alasan dan tautan kebijakan; tombol Buat tiket membawa nomor pesanan.
- [ ] Pelanggan tidak melihat menu atau halaman admin; Admin dan Developer diarahkan ke dashboard masing-masing.
- [ ] Nomor `ORD-`, `TKT-`, `PRD-`, `KB-` di chat dapat diklik menuju halaman yang benar.
- [ ] Tampilan nyaman di layar ponsel (≥ 360 px) dan dapat dioperasikan dengan keyboard.
