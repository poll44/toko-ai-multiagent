package knowledge // data awal (seed) dokumen kebijakan

import ( // package yang dipakai
	"context" // context
	"strings" // menggabungkan paragraf

	"toko-ai-microservices/service-ticket/internal/model" // tipe data
	"toko-ai-microservices/service-ticket/internal/store" // repository
)

// seedDoc: bentuk data satu dokumen kebijakan seed.
type seedDoc struct {
	Title, Category string   // judul & kategori
	Paras           []string // paragraf isi dokumen
}

// 8 dokumen kebijakan DEMO (fiktif). Wajib konsisten dengan service-order: batas bayar 15 menit; stok dikembalikan
// saat gagal/kedaluwarsa; total pesanan = total harga barang (tanpa ongkir terpisah).
var seedDocs = []seedDoc{
	// RETURN: Syarat dan Prosedur Retur
	{"Syarat dan Prosedur Retur", "RETURN", []string{
		"Pelanggan dapat mengajukan retur atau pengembalian barang paling lambat 7 hari setelah barang diterima (status pesanan DELIVERED). Barang harus belum dipakai, kemasan dan kelengkapan masih utuh, dan disertai bukti pembelian berupa nomor pesanan.",
		"Untuk barang cacat, rusak, atau salah kirim, pelanggan wajib melampirkan foto atau video unboxing sebagai bukti. Ongkos kirim retur ditanggung toko jika barang cacat atau salah kirim, dan ditanggung pembeli jika alasan retur adalah berubah pikiran.",
		"Produk elektronik yang sudah diaktifkan atau digunakan tidak dapat diretur, kecuali terbukti cacat pabrik. Pengajuan retur dilakukan dengan membuat tiket kategori PRODUCT dan mencantumkan nomor pesanan. Setelah disetujui, proses pengembalian barang dan refund mengikuti kebijakan refund.",
	}},
	// WARRANTY: Kebijakan Garansi
	{"Kebijakan Garansi", "WARRANTY", []string{
		"Setiap produk elektronik dilindungi garansi toko selama 12 bulan sejak barang diterima untuk kerusakan akibat cacat pabrik. Aksesoris seperti kabel dan charger memiliki garansi 6 bulan.",
		"Garansi tidak mencakup kerusakan akibat benda jatuh, terkena cairan, modifikasi, atau pemakaian yang tidak sesuai petunjuk. Klaim garansi diajukan dengan membuat tiket kategori PRODUCT beserta nomor pesanan dan deskripsi kerusakan.",
		"Proses klaim garansi memerlukan 3 sampai 14 hari kerja, tergantung hasil pemeriksaan teknisi dan ketersediaan suku cadang atau unit pengganti.",
	}},
	// SHIPPING: Estimasi Pengiriman
	{"Estimasi Pengiriman", "SHIPPING", []string{
		"Pesanan mulai diproses setelah pembayaran berhasil (status PAID). Estimasi waktu pengiriman: Jabodetabek 1 sampai 2 hari kerja, Pulau Jawa lainnya 2 sampai 4 hari kerja, dan luar Pulau Jawa 4 sampai 7 hari kerja sejak pesanan dikirim.",
		"Nomor resi diterbitkan saat status pesanan menjadi SHIPPED dan dapat dilihat pada detail pesanan. Pada versi demo ini tidak ada ongkos kirim terpisah; total pesanan sama dengan total harga barang.",
		"Jika pesanan belum sampai melewati estimasi lebih dari 3 hari kerja, pelanggan dapat membuat tiket kategori SHIPPING dengan nomor pesanan agar tim menelusuri paket.",
	}},
	// COMPLAINT: Prosedur Komplain
	{"Prosedur Komplain", "COMPLAINT", []string{
		"Langkah pertama komplain adalah memeriksa status pesanan dan kebijakan toko yang terkait (retur, garansi, pengiriman, pembayaran). Banyak masalah dapat dijelaskan oleh status pesanan atau kebijakan tersebut.",
		"Jika masalah belum terselesaikan, buat tiket komplain dengan subjek yang jelas, kategori yang sesuai (PAYMENT, SHIPPING, PRODUCT, TECHNICAL, atau OTHER), dan nomor pesanan bila ada. Tim memberikan respons awal paling lama 1 x 24 jam kerja.",
		"Status tiket berjalan dari OPEN, IN_PROGRESS, RESOLVED, lalu CLOSED. Tiket yang sudah RESOLVED dapat dibuka kembali oleh pelanggan dengan menyertakan alasan, atau ditutup bila masalah selesai. Satu pesanan hanya dapat memiliki satu tiket aktif pada satu waktu.",
	}},
	// PAYMENT: Pembayaran Gagal dan Kedaluwarsa
	{"Pembayaran Gagal dan Kedaluwarsa", "PAYMENT", []string{
		"Setiap pesanan baru berstatus PENDING_PAYMENT dengan batas waktu pembayaran 15 menit. Jika pembayaran tidak diselesaikan dalam waktu tersebut, pesanan otomatis menjadi PAYMENT_TIMEOUT karena batas waktu bayar terlewati.",
		"Pembayaran yang ditolak penyedia pembayaran, misalnya karena saldo e-wallet tidak mencukupi, membuat pesanan berstatus PAYMENT_FAILED. Pada kedua kasus (gagal maupun kedaluwarsa) stok produk dikembalikan ke katalog, pesanan bersifat final, dan tidak dapat dibayar ulang. Pelanggan perlu membuat pesanan baru.",
		"Dana tidak boleh terpotong untuk pembayaran yang gagal. Jika saldo atau rekening Anda terpotong padahal pesanan berstatus PAYMENT_FAILED atau PAYMENT_TIMEOUT, buat tiket kategori PAYMENT dengan prioritas HIGH dan lampirkan bukti pemotongan.",
	}},
	// ORDER: Pembatalan Pesanan
	{"Pembatalan Pesanan", "ORDER", []string{
		"Pelanggan dapat membatalkan pesanan sendiri hanya selama statusnya masih PENDING_PAYMENT. Stok produk akan dikembalikan setelah pembatalan.",
		"Pesanan yang sudah dibayar (PAID atau PROCESSING) hanya dapat dibatalkan oleh admin; ajukan permintaan melalui tiket. Pembatalan oleh admin atas pesanan yang sudah dibayar mengubah status pembayaran menjadi REFUNDED. Pesanan yang sudah SHIPPED tidak dapat dibatalkan; gunakan prosedur retur setelah barang diterima.",
	}},
	// PAYMENT: Kebijakan Refund
	{"Kebijakan Refund", "PAYMENT", []string{
		"Refund dikembalikan ke metode pembayaran asal dalam 3 sampai 7 hari kerja setelah refund disetujui.",
		"Pesanan yang dibatalkan admin setelah dibayar otomatis berstatus REFUNDED. Untuk retur, refund diproses setelah barang diterima dan lolos pemeriksaan kondisi. Toko tidak memberikan kompensasi di luar refund yang tercantum dalam kebijakan ini.",
	}},
	// GENERAL: Jam Layanan dan Kontak
	{"Jam Layanan dan Kontak", "GENERAL", []string{
		"Layanan pelanggan beroperasi Senin sampai Jumat pukul 09.00 sampai 17.00 WIB, kecuali hari libur nasional.",
		"Di luar jam layanan, tiket tetap diterima dan akan diproses pada hari kerja berikutnya. Kanal resmi satu-satunya adalah tiket di aplikasi dan Asisten AI; toko tidak melayani komplain melalui kanal lain.",
	}},
}

// SeedDocs mengisi 8 dokumen kebijakan (KB-001..KB-008) + chunk bila tabel kosong (idempoten). Embedding dibuat indexer.
func SeedDocs(ctx context.Context, repo *store.Repo) error {
	existing, err := repo.ListDocs(ctx, true) // dokumen yang sudah ada
	if err != nil {
		return err
	}
	if len(existing) > 0 { // sudah terisi: tidak perlu seed lagi
		return nil
	}
	for _, d := range seedDocs { // simpan tiap dokumen
		content := strings.Join(d.Paras, "\n\n") // gabungkan paragraf
		if _, err := repo.CreateDoc(ctx, model.DocumentInput{Title: d.Title, Category: d.Category, Content: content, IsActive: true},
			Split(content), Hash); err != nil { // pecah jadi chunk & simpan
			return err
		}
	}
	return nil
}
