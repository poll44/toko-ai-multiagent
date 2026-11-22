package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/sqlitex"
	"toko-ai-microservices/service-catalog/internal/model"
)

type Repo struct{ db *sql.DB }

func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

const productCols = `id,name,description,specifications,category,price,stock,image_url,is_active,created_at,updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanProduct(sc scanner) (model.Product, error) {
	var p model.Product
	var img sql.NullString
	var active int
	if err := sc.Scan(&p.ID, &p.Name, &p.Description, &p.Specifications, &p.Category,
		&p.Price, &p.Stock, &img, &active, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, err
	}
	if img.Valid {
		s := img.String
		p.ImageURL = &s
	}
	p.IsActive = active == 1
	p.StockStatus = model.StockStatusOf(p.Stock)
	return p, nil
}

func notFound(id string) error { return httputil.ErrNotFound("Produk " + id + " tidak ditemukan.") }

// escapeLike meng-escape %, _ dan \ untuk LIKE ... ESCAPE '\'.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// filterClauses membangun WHERE bersama (kategori, harga, stok, aktif).
func filterClauses(f model.ListFilter) ([]string, []any) {
	where := []string{"1=1"}
	var args []any
	if !f.IncludeInactive {
		where = append(where, "is_active = 1")
	}
	if f.Category != "" {
		where = append(where, "category = ?")
		args = append(args, f.Category)
	}
	if f.MinPrice != nil {
		where = append(where, "price >= ?")
		args = append(args, *f.MinPrice)
	}
	if f.MaxPrice != nil {
		where = append(where, "price <= ?")
		args = append(args, *f.MaxPrice)
	}
	if f.InStock != nil && *f.InStock {
		where = append(where, "stock > 0")
	}
	return where, args
}

func orderBy(sort string) string {
	switch sort {
	case "price_asc":
		return "price ASC, name COLLATE NOCASE ASC"
	case "price_desc":
		return "price DESC, name COLLATE NOCASE ASC"
	case "newest":
		return "created_at DESC, id DESC"
	default:
		return "name COLLATE NOCASE ASC"
	}
}

const likeCols = `(LOWER(name) LIKE ? ESCAPE '\' OR LOWER(description) LIKE ? ESCAPE '\' OR LOWER(specifications) LIKE ? ESCAPE '\')`

// List: pencarian eksak (LIKE) + filter + paginasi. Mengembalikan slice non-nil.
func (r *Repo) List(ctx context.Context, f model.ListFilter) ([]model.Product, int, error) {
	where, args := filterClauses(f)
	if q := strings.ToLower(strings.TrimSpace(f.Q)); q != "" {
		like := "%" + escapeLike(q) + "%"
		where = append(where, likeCols)
		args = append(args, like, like, like)
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products WHERE "+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := fmt.Sprintf("SELECT %s FROM products WHERE %s ORDER BY %s LIMIT ? OFFSET ?", productCols, w, orderBy(f.Sort))
	rows, err := r.db.QueryContext(ctx, q, append(args, f.Limit, (f.Page-1)*f.Limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.Product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

var stopwords = map[string]bool{"yang": true, "tidak": true, "dan": true, "untuk": true, "dengan": true, "bikin": true, "buat": true, "atau": true, "dari": true, "yang ": true}

// KeywordSearch: fallback bila embedding tak tersedia — OR per kata kunci (>=3 huruf, bukan stopword),
// hanya produk aktif. Dipakai dengan meta.mode="keyword_fallback".
func (r *Repo) KeywordSearch(ctx context.Context, q string, f model.ListFilter, limit int) ([]model.Product, error) {
	f.IncludeInactive = false
	where, args := filterClauses(f)
	var ors []string
	for _, tok := range strings.Fields(strings.ToLower(q)) {
		tok = strings.Trim(tok, `.,;:!?"'()`)
		if len([]rune(tok)) < 3 || stopwords[tok] {
			continue
		}
		like := "%" + escapeLike(tok) + "%"
		ors = append(ors, likeCols)
		args = append(args, like, like, like)
	}
	out := []model.Product{}
	if len(ors) == 0 {
		return out, nil
	}
	where = append(where, "("+strings.Join(ors, " OR ")+")")
	query := fmt.Sprintf("SELECT %s FROM products WHERE %s ORDER BY name COLLATE NOCASE ASC LIMIT ?", productCols, strings.Join(where, " AND "))
	rows, err := r.db.QueryContext(ctx, query, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, id string) (*model.Product, error) {
	p, err := scanProduct(r.db.QueryRowContext(ctx, "SELECT "+productCols+" FROM products WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
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
	defer rows.Close()
	var out []model.Product
	for rows.Next() {
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
	defer rows.Close()
	m := map[string]model.Product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		m[p.ID] = p
	}
	return m, rows.Err()
}

func (r *Repo) Categories(ctx context.Context) ([]model.Category, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT category, COUNT(*) FROM products WHERE is_active = 1 GROUP BY category ORDER BY category")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Category{}
	for rows.Next() {
		var c model.Category
		if err := rows.Scan(&c.Category, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func addMovement(ctx context.Context, tx *sql.Tx, productID string, delta, after int, reason string, ref *string, actor string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO stock_movements (product_id,delta,stock_after,reason,reference_id,actor_id,created_at) VALUES (?,?,?,?,?,?,?)`,
		productID, delta, after, reason, ref, actor, sqlitex.NowUTC())
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Create membuat produk baru (ID berurutan PRD-xxx). Stok awal > 0 dicatat sebagai ADMIN_ADJUST.
func (r *Repo) Create(ctx context.Context, in model.ProductInput, actor string) (*model.Product, error) {
	var id string
	err := sqlitex.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		var maxN int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(SUBSTR(id,5) AS INTEGER)),0) FROM products`).Scan(&maxN); err != nil {
			return err
		}
		id = fmt.Sprintf("PRD-%03d", maxN+1)
		now := sqlitex.NowUTC()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO products (id,name,description,specifications,category,price,stock,image_url,is_active,created_at,updated_at)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			id, in.Name, in.Description, in.Specifications, in.Category, in.Price, in.Stock, in.ImageURL, boolInt(in.IsActive), now, now); err != nil {
			return err
		}
		if in.Stock > 0 {
			return addMovement(ctx, tx, id, in.Stock, in.Stock, "ADMIN_ADJUST", nil, actor)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

// Update mengubah field produk (tanpa stok). Mengembalikan produk lama agar pemanggil tahu teks embedding berubah.
func (r *Repo) Update(ctx context.Context, id string, in model.ProductInput) (*model.Product, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE products SET name=?,description=?,specifications=?,category=?,price=?,image_url=?,is_active=?,updated_at=? WHERE id=?`,
		in.Name, in.Description, in.Specifications, in.Category, in.Price, in.ImageURL, boolInt(in.IsActive), sqlitex.NowUTC(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, notFound(id)
	}
	return r.Get(ctx, id)
}

// SoftDelete: is_active=0 (idempoten). Produk tak ada => 404.
func (r *Repo) SoftDelete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE products SET is_active=0, updated_at=? WHERE id=?`, sqlitex.NowUTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound(id)
	}
	return nil
}

func (r *Repo) Movements(ctx context.Context, id string) ([]model.StockMovement, error) {
	if _, err := r.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id,product_id,delta,stock_after,reason,reference_id,actor_id,created_at FROM stock_movements WHERE product_id=? ORDER BY id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.StockMovement{}
	for rows.Next() {
		var m model.StockMovement
		var ref sql.NullString
		if err := rows.Scan(&m.ID, &m.ProductID, &m.Delta, &m.StockAfter, &m.Reason, &ref, &m.ActorID, &m.CreatedAt); err != nil {
			return nil, err
		}
		if ref.Valid {
			s := ref.String
			m.ReferenceID = &s
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
