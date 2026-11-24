package store // repository akses data produk

import ( // package yang dipakai
	"context"      // context
	"database/sql" // API database
	"errors"       // errors.Is
	"fmt"          // format string
	"strings"      // utilitas string

	"toko-ai-microservices/internal/shared/httputil"       // error API standar
	"toko-ai-microservices/internal/shared/sqlitex"        // transaksi & waktu
	"toko-ai-microservices/service-catalog/internal/model" // tipe data
)

// Repo membungkus koneksi database catalog.db.
type Repo struct{ db *sql.DB }

// NewRepo membuat Repo dari koneksi database.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

// productCols: daftar kolom produk (urutan harus sama dengan scanProduct).
const productCols = `id,name,description,specifications,category,price,stock,image_url,is_active,created_at,updated_at`

// scanner: abstraksi untuk *sql.Row dan *sql.Rows (keduanya punya Scan).
type scanner interface{ Scan(dest ...any) error }

// scanProduct membaca satu baris produk ke model.Product.
func scanProduct(sc scanner) (model.Product, error) {
	var p model.Product    // hasil
	var img sql.NullString // image_url bisa NULL
	var active int         // is_active tersimpan sebagai 0/1
	if err := sc.Scan(&p.ID, &p.Name, &p.Description, &p.Specifications, &p.Category,
		&p.Price, &p.Stock, &img, &active, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, err
	}
	if img.Valid { // ada URL gambar
		s := img.String
		p.ImageURL = &s
	}
	p.IsActive = active == 1                     // ubah 0/1 menjadi bool
	p.StockStatus = model.StockStatusOf(p.Stock) // hitung status stok
	return p, nil
}

// notFound membuat error 404 untuk produk id.
func notFound(id string) error { return httputil.ErrNotFound("Produk " + id + " tidak ditemukan.") }

// escapeLike meng-escape %, _ dan \ untuk LIKE ... ESCAPE '\'.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`) // tiap karakter khusus diberi awalan backslash
	return r.Replace(s)
}

// filterClauses membangun WHERE bersama (kategori, harga, stok, aktif).
func filterClauses(f model.ListFilter) ([]string, []any) {
	where := []string{"1=1"} // kondisi dasar agar mudah digabung dengan AND
	var args []any           // argumen untuk placeholder
	if !f.IncludeInactive {  // sembunyikan produk nonaktif
		where = append(where, "is_active = 1")
	}
	if f.Category != "" { // filter kategori
		where = append(where, "category = ?")
		args = append(args, f.Category)
	}
	if f.MinPrice != nil { // harga minimum
		where = append(where, "price >= ?")
		args = append(args, *f.MinPrice)
	}
	if f.MaxPrice != nil { // harga maksimum
		where = append(where, "price <= ?")
		args = append(args, *f.MaxPrice)
	}
	if f.InStock != nil && *f.InStock { // hanya yang ada stok
		where = append(where, "stock > 0")
	}
	return where, args
}

// orderBy menerjemahkan nama sort menjadi klausa ORDER BY (whitelist, aman dari injection).
func orderBy(sort string) string {
	switch sort {
	case "price_asc": // termurah dulu
		return "price ASC, name COLLATE NOCASE ASC"
	case "price_desc": // termahal dulu
		return "price DESC, name COLLATE NOCASE ASC"
	case "newest": // terbaru dulu
		return "created_at DESC, id DESC"
	default: // default: nama A-Z
		return "name COLLATE NOCASE ASC"
	}
}

// likeCols: kondisi LIKE pada nama, deskripsi, dan spesifikasi (membutuhkan 3 argumen).
const likeCols = `(LOWER(name) LIKE ? ESCAPE '\' OR LOWER(description) LIKE ? ESCAPE '\' OR LOWER(specifications) LIKE ? ESCAPE '\')`

// List: pencarian eksak (LIKE) + filter + paginasi. Mengembalikan slice non-nil.
func (r *Repo) List(ctx context.Context, f model.ListFilter) ([]model.Product, int, error) {
	where, args := filterClauses(f)                            // filter dasar
	if q := strings.ToLower(strings.TrimSpace(f.Q)); q != "" { // ada kata kunci
		like := "%" + escapeLike(q) + "%"     // pola LIKE aman
		where = append(where, likeCols)       // tambah kondisi pencarian
		args = append(args, like, like, like) // 3 argumen untuk 3 kolom
	}
	w := strings.Join(where, " AND ") // gabungkan semua kondisi
	var total int                     // total baris (untuk paginasi)
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products WHERE "+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := fmt.Sprintf("SELECT %s FROM products WHERE %s ORDER BY %s LIMIT ? OFFSET ?", productCols, w, orderBy(f.Sort)) // query data
	rows, err := r.db.QueryContext(ctx, q, append(args, f.Limit, (f.Page-1)*f.Limit)...)                               // tambah LIMIT & OFFSET
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()       // tutup cursor saat selesai
	out := []model.Product{} // slice non-nil agar JSON jadi []
	for rows.Next() {        // baca tiap baris
		p, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// stopwords: kata umum yang diabaikan pada keyword fallback.
var stopwords = map[string]bool{"yang": true, "tidak": true, "dan": true, "untuk": true, "dengan": true, "bikin": true, "buat": true, "atau": true, "dari": true, "yang ": true}

// KeywordSearch: fallback bila embedding tak tersedia — OR per kata kunci (>=3 huruf, bukan stopword),
// hanya produk aktif. Dipakai dengan meta.mode="keyword_fallback".
func (r *Repo) KeywordSearch(ctx context.Context, q string, f model.ListFilter, limit int) ([]model.Product, error) {
	f.IncludeInactive = false                                // fallback hanya untuk produk aktif
	where, args := filterClauses(f)                          // filter dasar
	var ors []string                                         // kondisi LIKE per kata
	for _, tok := range strings.Fields(strings.ToLower(q)) { // pecah query menjadi kata
		tok = strings.Trim(tok, `.,;:!?"'()`)       // buang tanda baca
		if len([]rune(tok)) < 3 || stopwords[tok] { // terlalu pendek / stopword
			continue
		}
		like := "%" + escapeLike(tok) + "%"   // pola LIKE aman
		ors = append(ors, likeCols)           // tambah kondisi
		args = append(args, like, like, like) // 3 argumen
	}
	out := []model.Product{} // hasil non-nil
	if len(ors) == 0 {       // tidak ada kata kunci berguna
		return out, nil
	}
	where = append(where, "("+strings.Join(ors, " OR ")+")") // kata-kata digabung dengan OR
	query := fmt.Sprintf("SELECT %s FROM products WHERE %s ORDER BY name COLLATE NOCASE ASC LIMIT ?", productCols, strings.Join(where, " AND "))
	rows, err := r.db.QueryContext(ctx, query, append(args, limit)...) // jalankan query
	if err != nil {
		return nil, err
	}
	defer rows.Close() // tutup cursor saat selesai
	for rows.Next() {  // baca tiap baris
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Get mengambil satu produk berdasarkan ID (404 bila tidak ada).
func (r *Repo) Get(ctx context.Context, id string) (*model.Product, error) {
	p, err := scanProduct(r.db.QueryRowContext(ctx, "SELECT "+productCols+" FROM products WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) { // tidak ditemukan
		return nil, notFound(id)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// AllProducts: semua produk (termasuk nonaktif) — untuk indexing.
func (r *Repo) AllProducts(ctx context.Context) ([]model.Product, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+productCols+" FROM products ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()      // tutup cursor saat selesai
	var out []model.Product // hasil
	for rows.Next() {       // baca tiap baris
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ActiveProductsMap: produk aktif by id (harga & stok segar untuk hasil semantik).
func (r *Repo) ActiveProductsMap(ctx context.Context) (map[string]model.Product, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+productCols+" FROM products WHERE is_active = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()              // tutup cursor saat selesai
	m := map[string]model.Product{} // hasil: id -> produk
	for rows.Next() {               // baca tiap baris
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		m[p.ID] = p
	}
	return m, rows.Err()
}

// Categories mengembalikan kategori produk aktif beserta jumlahnya.
func (r *Repo) Categories(ctx context.Context) ([]model.Category, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT category, COUNT(*) FROM products WHERE is_active = 1 GROUP BY category ORDER BY category")
	if err != nil {
		return nil, err
	}
	defer rows.Close()        // tutup cursor saat selesai
	out := []model.Category{} // hasil non-nil
	for rows.Next() {         // baca tiap baris
		var c model.Category
		if err := rows.Scan(&c.Category, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// addMovement mencatat satu baris riwayat stok di dalam transaksi tx.
func addMovement(ctx context.Context, tx *sql.Tx, productID string, delta, after int, reason string, ref *string, actor string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO stock_movements (product_id,delta,stock_after,reason,reference_id,actor_id,created_at) VALUES (?,?,?,?,?,?,?)`,
		productID, delta, after, reason, ref, actor, sqlitex.NowUTC())
	return err
}

// boolInt mengubah bool menjadi 1/0 untuk disimpan di SQLite.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Create membuat produk baru (ID berurutan PRD-xxx). Stok awal > 0 dicatat sebagai ADMIN_ADJUST.
func (r *Repo) Create(ctx context.Context, in model.ProductInput, actor string) (*model.Product, error) {
	var id string                                             // ID produk baru
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error { // satu transaksi
		var maxN int // nomor urut terbesar saat ini
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(SUBSTR(id,5) AS INTEGER)),0) FROM products`).Scan(&maxN); err != nil {
			return err
		}
		id = fmt.Sprintf("PRD-%03d", maxN+1) // ID berikutnya
		now := sqlitex.NowUTC()              // waktu pembuatan
		if _, err := tx.ExecContext(ctx,
			// INSERT produk baru
			`INSERT INTO products (id,name,description,specifications,category,price,stock,image_url,is_active,created_at,updated_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			id, in.Name, in.Description, in.Specifications, in.Category, in.Price, in.Stock, in.ImageURL, boolInt(in.IsActive), now, now); err != nil {
			return err
		}
		if in.Stock > 0 { // catat stok awal
			return addMovement(ctx, tx, id, in.Stock, in.Stock, "ADMIN_ADJUST", nil, actor)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id) // kembalikan produk yang baru dibuat
}

// Update mengubah field produk (tanpa stok). Mengembalikan produk lama agar pemanggil tahu teks embedding berubah.
func (r *Repo) Update(ctx context.Context, id string, in model.ProductInput) (*model.Product, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE products SET name=?,description=?,specifications=?,category=?,price=?,image_url=?,is_active=?,updated_at=? WHERE id=?`,
		in.Name, in.Description, in.Specifications, in.Category, in.Price, in.ImageURL, boolInt(in.IsActive), sqlitex.NowUTC(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 { // tidak ada baris yang berubah => produk tidak ada
		return nil, notFound(id)
	}
	return r.Get(ctx, id) // kembalikan produk terbaru
}

// SoftDelete: is_active=0 (idempoten). Produk tak ada => 404.
func (r *Repo) SoftDelete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE products SET is_active=0, updated_at=? WHERE id=?`, sqlitex.NowUTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 { // produk tidak ada
		return notFound(id)
	}
	return nil
}

// Movements mengembalikan riwayat stok sebuah produk (terbaru dulu).
func (r *Repo) Movements(ctx context.Context, id string) ([]model.StockMovement, error) {
	if _, err := r.Get(ctx, id); err != nil { // pastikan produk ada
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id,product_id,delta,stock_after,reason,reference_id,actor_id,created_at FROM stock_movements WHERE product_id=? ORDER BY id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()             // tutup cursor saat selesai
	out := []model.StockMovement{} // hasil non-nil
	for rows.Next() {              // baca tiap baris
		var m model.StockMovement
		var ref sql.NullString // reference_id bisa NULL
		if err := rows.Scan(&m.ID, &m.ProductID, &m.Delta, &m.StockAfter, &m.Reason, &ref, &m.ActorID, &m.CreatedAt); err != nil {
			return nil, err
		}
		if ref.Valid { // ada referensi
			s := ref.String
			m.ReferenceID = &s
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
