# 03 — PRD Pesanan & Pembayaran

| Item | Nilai |
|---|---|
| Rujukan | [`00-MASTER-PRD.md`](00-MASTER-PRD.md) |
| Spesifikasi teknis | [`TRD 03 Order`](../trd/03-SERVICE-ORDER.md) |

## 1. Tujuan
Pelanggan dapat memesan barang, membayar, memantau, dan membatalkan pesanan dengan jelas — termasuk memahami **mengapa** sebuah pesanan gagal. Admin dapat memproses pesanan sampai terkirim.

## 2. Status Pesanan

| Status | Arti bagi pengguna | Final? |
|---|---|:-:|
| Menunggu pembayaran | Pesanan dibuat; ada batas waktu bayar 15 menit | — |
| Dibayar | Pembayaran berhasil | — |
| Diproses | Admin sedang menyiapkan barang | — |
| Dikirim | Barang dikirim; nomor resi tersedia | — |
| Diterima | Barang sampai | ✅ |
| Dibatalkan | Dibatalkan pelanggan atau admin | ✅ |
| Pembayaran gagal | Pembayaran ditolak; alasan tercatat | ✅ |
| Pembayaran kedaluwarsa | Batas waktu bayar terlewati | ✅ |

```
Menunggu pembayaran ─bayar berhasil─► Dibayar ─► Diproses ─► Dikirim ─► Diterima
   ├─ bayar gagal ───────► Pembayaran gagal      (stok kembali)
   ├─ lewat 15 menit ────► Pembayaran kedaluwarsa (stok kembali)
   └─ batal ─────────────► Dibatalkan             (stok kembali)
Dibayar / Diproses ─batal oleh admin─► Dibatalkan (dana dikembalikan, stok kembali)
```

Status akhir tidak dapat diubah lagi. Pesanan yang gagal atau kedaluwarsa bersifat **final**: pelanggan membuat pesanan baru.

## 3. Kebutuhan Fungsional

### 3.1 Pelanggan
| ID | Kebutuhan | Prioritas |
|---|---|---|
| O-01 | Pelanggan dapat mengumpulkan barang di **keranjang** (tersimpan di perangkat) dan melihat total perkiraan; harga final ditetapkan sistem saat pesanan dibuat | Wajib |
| O-02 | Pelanggan dapat membuat pesanan berisi 1–20 jenis barang (jumlah 1–99 per barang) dengan metode pembayaran (e-wallet, transfer bank, kartu kredit) dan alamat pengiriman (10–300 karakter) | Wajib |
| O-03 | Pesanan tidak dibuat bila ada barang yang stoknya kurang, tidak ditemukan, atau nonaktif; pesan galat menyebut barang, stok tersedia, dan jumlah diminta | Wajib |
| O-04 | Setelah dibuat, pesanan tampil dengan hitung mundur batas bayar | Wajib |
| O-05 | Pelanggan dapat membayar pesanannya. Karena pembayaran hanya simulasi, tersedia pilihan hasil: **berhasil**, **gagal**, atau **kedaluwarsa** | Wajib |
| O-06 | Pelanggan dapat membatalkan pesanan miliknya **selama belum dibayar** | Wajib |
| O-07 | Pelanggan dapat melihat daftar pesanan miliknya (terbaru dulu, dapat difilter status) dan detail pesanan beserta riwayat perubahan status | Wajib |
| O-08 | Detail pesanan menampilkan alasan gagal (`failure_reason`) apa adanya, nomor resi (bila dikirim), dan tautan membuat tiket | Wajib |
| O-09 | Untuk pesanan gagal/kedaluwarsa, tampil tautan ke kebijakan pembayaran gagal di Pusat Bantuan | Sebaiknya |

### 3.2 Admin & Developer
| ID | Kebutuhan | Prioritas |
|---|---|---|
| O-10 | Admin dapat melihat semua pesanan dan memfilter berdasarkan status atau pelanggan | Wajib |
| O-11 | Admin dapat memajukan status: Dibayar → Diproses → Dikirim → Diterima; **nomor resi wajib** saat Dikirim (5–40 karakter) | Wajib |
| O-12 | Admin dapat membatalkan pesanan yang belum dibayar, sudah dibayar, atau sedang diproses; pesanan yang sudah dibayar ditandai dana dikembalikan | Wajib |
| O-13 | Developer dapat melihat semua pesanan tetapi tidak dapat mengubah apa pun | Wajib |

### 3.3 Otomatis oleh sistem
| ID | Kebutuhan | Prioritas |
|---|---|---|
| O-14 | Pesanan menunggu pembayaran yang melewati batas waktu otomatis menjadi **Pembayaran kedaluwarsa**, stok dikembalikan, dan alasan tercatat | Wajib |
| O-15 | Setiap perubahan status tercatat (dari, ke, catatan, pelaku — termasuk "sistem") | Wajib |

## 4. Aturan Bisnis
1. Batas waktu bayar: **15 menit** sejak pesanan dibuat.
2. Stok dikurangi saat pesanan dibuat dan dikembalikan saat gagal bayar, kedaluwarsa, atau dibatalkan (lihat PRD 02 §4).
3. Total pesanan = jumlah subtotal; harga dan nama barang di-*snapshot* saat pesanan dibuat.
4. Pesanan selalu milik pengguna yang membuatnya; pemilik tidak bisa ditentukan dari input.
5. Membayar pesanan yang sudah lewat batas waktu membuat pesanan menjadi kedaluwarsa dan pembayaran ditolak.
6. Transisi status di luar diagram ditolak dengan pesan "perubahan status tidak sah".
7. Bila katalog tidak dapat dihubungi saat pesanan dibuat, **tidak ada pesanan yang tersimpan** dan stok tidak berubah.

## 5. Data Demo
| Pesanan | Pemilik | Status | Tujuan uji |
|---|---|---|---|
| `ORD-001` | Budi | Diterima | Riwayat normal |
| `ORD-002` | Budi | Dikirim (resi `JNE-TRK-0002`) | Pelacakan |
| `ORD-003` | Budi | Pembayaran gagal — "Saldo e-wallet tidak mencukupi" | **Skenario komplain utama** |
| `ORD-004` | Budi | Pembayaran kedaluwarsa | Skenario batas waktu |
| `ORD-005` | Siti | Diproses | Uji isolasi data |
| `ORD-006` | Siti | Menunggu pembayaran (sisa ±10 menit) | Uji kedaluwarsa otomatis |
| `ORD-007` | Siti | Dibatalkan | Pembatalan oleh pelanggan |
| `ORD-008` | Siti | Diterima | Riwayat normal |

## 6. Kriteria Diterima
- [ ] Pesanan sukses mengurangi stok tepat sebesar jumlah pesanan.
- [ ] Jumlah melebihi stok → pesanan ditolak, tidak ada pesanan tersimpan, stok tidak berubah.
- [ ] Harga yang dikirim dari klien diabaikan; total berasal dari katalog.
- [ ] Siti membuka `ORD-003` → "tidak ditemukan"; daftar pesanan Budi hanya `ORD-001`–`ORD-004`.
- [ ] Developer dapat melihat semua pesanan tetapi tidak dapat membayar atau mengubahnya.
- [ ] Bayar dengan hasil "gagal" → status Pembayaran gagal dan stok kembali.
- [ ] `ORD-006` otomatis kedaluwarsa setelah ±10 menit dan stok `PRD-002` bertambah 1.
- [ ] Pengiriman tanpa nomor resi ditolak.
- [ ] Status akhir tidak dapat diubah; pembatalan oleh pelanggan setelah dibayar ditolak.
- [ ] Setiap perubahan status muncul pada riwayat pesanan.
