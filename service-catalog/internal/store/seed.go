package store

import (
	"context"
	"database/sql"

	"toko-ai-microservices/internal/shared/sqlitex"
)

type seedProduct struct {
	ID, Name, Category, Description, Specs string
	Price, Stock                           int
	Active                                 bool
}

// 14 produk seed (PRD 02 §4). PRD-010 stok 0, PRD-012 stok menipis, PRD-013 nonaktif, PRD-014 target uji semantik.
var seedProducts = []seedProduct{
	{"PRD-001", "Earbuds Nova X1 TWS", "audio", "Earbuds nirkabel true wireless dengan peredam bising aktif untuk musik dan panggilan.",
		"Bluetooth 5.3; ANC hingga 30 dB; baterai 8 jam + case 24 jam; tahan air IPX4; driver 10 mm.", 349000, 50, true},
	{"PRD-002", "Headphone Bass Pro H200", "audio", "Headphone over-ear dengan bass dalam dan bantalan empuk, nyaman untuk mendengarkan musik berjam-jam di rumah atau perjalanan.",
		"Driver 40 mm; Bluetooth 5.2 + kabel 3,5 mm; baterai 40 jam; bisa dilipat; mikrofon bawaan.", 799000, 25, true},
	{"PRD-003", "Powerbank 20000mAh FastCharge", "aksesoris", "Powerbank kapasitas besar dengan pengisian cepat untuk smartphone dan tablet, cocok dibawa bepergian.",
		"Kapasitas 20000mAh; output 22.5W; 2×USB-A + 1×USB-C; layar indikator digital.", 299000, 80, true},
	{"PRD-004", "Kabel USB-C to USB-C 1m 100W", "aksesoris", "Kabel USB-C ke USB-C sepanjang 1 meter, mendukung pengisian daya cepat hingga 100W dan transfer data.",
		"Panjang 1 m; daya hingga 100W (PD 3.0); data 480 Mbps; kabel braided tahan tekuk.", 89000, 200, true},
	{"PRD-005", "Charger GaN 65W 3 Port", "aksesoris", "Charger ringkas berteknologi GaN yang dapat mengisi laptop, tablet, dan ponsel sekaligus.",
		"Output total 65W; 2×USB-C + 1×USB-A; teknologi GaN; proteksi panas berlebih.", 249000, 60, true},
	{"PRD-006", "Smartwatch FitBand S5", "wearable", "Jam tangan pintar untuk memantau kebugaran, detak jantung, dan notifikasi harian.",
		"Layar AMOLED 1,4 inci; sensor detak jantung & SpO2; GPS; baterai 10 hari; tahan air 5 ATM.", 899000, 30, true},
	{"PRD-007", "Keyboard Mekanikal K87 RGB", "komputer", "Keyboard mekanikal tenkeyless dengan lampu RGB untuk mengetik dan gaming.",
		"87 tombol; switch hot-swap linear; backlight RGB; kabel USB-C; keycap PBT.", 649000, 20, true},
	{"PRD-008", "Mouse Wireless Silent M330", "komputer", "Mouse wireless klik senyap untuk ruang kerja tenang.",
		"Klik senyap; DPI 1000/1600; 2.4GHz; baterai AA 12 bulan; bentuk simetris standar.", 159000, 100, true},
	{"PRD-009", "Webcam Full HD 1080p", "komputer", "Webcam Full HD untuk rapat daring dan streaming dengan mikrofon bawaan.",
		"1080p 30 fps; sudut pandang 90°; autofokus; mikrofon ganda peredam bising; USB-A.", 329000, 15, true},
	{"PRD-010", "Speaker Bluetooth Boomer Mini", "audio", "Speaker Bluetooth mungil bertenaga untuk acara santai di luar ruangan.",
		"10W; Bluetooth 5.3; baterai 12 jam; tahan percikan air IPX5; slot kartu microSD.", 279000, 0, true},
	{"PRD-011", "Dudukan Smartphone Aluminium", "aksesoris", "Dudukan ponsel dari aluminium yang kokoh dengan sudut yang dapat diatur untuk meja kerja.",
		"Material aluminium; sudut 0–70°; bisa dilipat; kompatibel layar 4–12,9 inci; alas anti-selip.", 79000, 150, true},
	{"PRD-012", "SSD Eksternal 1TB", "penyimpanan", "SSD eksternal portabel berkecepatan tinggi untuk cadangan data dan kerja kreatif.",
		"Kapasitas 1TB; baca hingga 1050 MB/s; USB 3.2 Gen2 Type-C; tahan guncangan; bodi aluminium.", 1299000, 8, true},
	{"PRD-013", "Tablet Lawas Seri A", "komputer", "Tablet generasi lama, sudah tidak dijual.",
		"Layar 8 inci; RAM 2GB; penyimpanan 32GB.", 1500000, 5, false},
	{"PRD-014", "Mouse Vertikal Ergonomis ErgoGlide V2", "komputer", "Mouse wireless dengan posisi genggaman vertikal seperti berjabat tangan, mengurangi tekanan pada pergelangan tangan sehingga tidak pegal dan nyaman dipakai berjam-jam.",
		"Desain vertikal ergonomis 57°; sandaran ibu jari; DPI 800/1200/1600; Bluetooth + 2.4GHz; baterai 3 bulan.", 289000, 35, true},
}

// Seed mengisi produk awal bila tabel kosong (idempoten). Embedding dibuat terpisah oleh indexer.
func (r *Repo) Seed(ctx context.Context) error {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM products`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		now := sqlitex.NowUTC()
		for _, p := range seedProducts {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO products (id,name,description,specifications,category,price,stock,image_url,is_active,created_at,updated_at)
				 VALUES (?,?,?,?,?,?,?,NULL,?,?,?)`,
				p.ID, p.Name, p.Description, p.Specs, p.Category, p.Price, p.Stock, boolInt(p.Active), now, now); err != nil {
				return err
			}
			if err := addMovement(ctx, tx, p.ID, p.Stock, p.Stock, "SEED", nil, "SYSTEM"); err != nil {
				return err
			}
		}
		return nil
	})
}
