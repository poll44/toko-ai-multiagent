package handlers // handler HTTP untuk produk

import ( // package yang dipakai
	"context"  // context
	"log/slog" // logging terstruktur
	"net/http" // tipe HTTP
	"net/url"  // parsing URL gambar
	"regexp"   // regular expression
	"time"     // timeout

	h "toko-ai-microservices/internal/shared/httputil"     // helper HTTP bersama
	"toko-ai-microservices/internal/shared/validate"       // validator
	"toko-ai-microservices/service-catalog/internal/model" // tipe data
)

var (
	idRe       = regexp.MustCompile(`^PRD-\d{3,}$`)      // format ID produk
	categoryRe = regexp.MustCompile(`^[a-z0-9-]{2,30}$`) // format kategori
)

const (
	maxPrice = 1_000_000_000 // harga maksimum
	maxStock = 1_000_000     // stok maksimum
)

// isAdmin: true bila pemanggil adalah admin.
func isAdmin(r *http.Request) bool { return h.IdentityFrom(r.Context()).IsAdmin() }

// productID memvalidasi {id}; format salah diperlakukan sebagai tidak ditemukan.
func productID(r *http.Request) (string, error) {
	id := r.PathValue("id")    // ambil {id} dari path
	if !idRe.MatchString(id) { // format salah
		return "", h.ErrNotFound("Produk " + id + " tidak ditemukan.")
	}
	return id, nil
}

// parseFilter membaca query filter bersama (eksak & semantik).
func parseFilter(r *http.Request) (model.ListFilter, error) {
	q := r.URL.Query()                                                                     // semua query param
	f := model.ListFilter{Q: q.Get("q"), Category: q.Get("category"), Sort: q.Get("sort")} // filter dasar
	v := validate.New()                                                                    // validator
	if f.Category != "" && !categoryRe.MatchString(f.Category) {                           // kategori tidak valid
		v.Add("category", "format kategori tidak valid")
	}
	if f.Sort != "" { // sort harus dari whitelist
		v.OneOf("sort", f.Sort, "name", "price_asc", "price_desc", "newest")
	}
	var err error
	if q.Get("min_price") != "" { // harga minimum
		n, e := h.QueryInt(r, "min_price", 0, 0, maxPrice)
		if e != nil {
			return f, e
		}
		f.MinPrice = &n
	}
	if q.Get("max_price") != "" { // harga maksimum
		n, e := h.QueryInt(r, "max_price", 0, 0, maxPrice)
		if e != nil {
			return f, e
		}
		f.MaxPrice = &n
	}
	if f.InStock, err = h.QueryBool(r, "in_stock"); err != nil { // filter stok
		return f, err
	}
	if e := v.Err(); e != nil { // ada error validasi
		return f, e
	}
	return f, nil
}

// listProducts: GET /api/products — daftar/cari produk dengan filter & paginasi.
func (d *Deps) listProducts(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r) // baca filter
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if f.Page, f.Limit, err = h.Pagination(r); err != nil { // baca page & limit
		h.Fail(w, r, err)
		return
	}
	inc, err := h.QueryBool(r, "include_inactive") // sertakan nonaktif?
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	f.IncludeInactive = inc != nil && *inc && isAdmin(r) // role lain diabaikan
	items, total, err := d.Repo.List(r.Context(), f)     // query database
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteList(w, items, h.PageMeta(total, f.Page, f.Limit)) // kirim daftar + meta
}

// categories: GET /api/products/categories — daftar kategori + jumlah produk.
func (d *Deps) categories(w http.ResponseWriter, r *http.Request) {
	cats, err := d.Repo.Categories(r.Context())
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, cats)
}

// getProduct: GET /api/products/{id} — detail satu produk.
func (d *Deps) getProduct(w http.ResponseWriter, r *http.Request) {
	id, err := productID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p, err := d.Repo.Get(r.Context(), id) // ambil produk
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if !p.IsActive && !isAdmin(r) { // jangan bocorkan eksistensi produk nonaktif
		h.Fail(w, r, h.ErrNotFound("Produk "+id+" tidak ditemukan."))
		return
	}
	h.WriteData(w, http.StatusOK, p)
}

// productBody: payload JSON untuk membuat/mengubah produk.
type productBody struct {
	Name           string  `json:"name"`           // nama
	Description    string  `json:"description"`    // deskripsi
	Specifications string  `json:"specifications"` // spesifikasi
	Category       string  `json:"category"`       // kategori
	Price          *int    `json:"price"`          // harga (pointer agar bisa dibedakan dari 0)
	Stock          *int    `json:"stock"`          // stok awal (hanya create)
	ImageURL       *string `json:"image_url"`      // URL gambar
	IsActive       *bool   `json:"is_active"`      // status aktif
}

// validate memvalidasi payload dan mengubahnya menjadi model.ProductInput.
func (b productBody) validate(create bool) (model.ProductInput, error) {
	v := validate.New()           // validator
	v.Len("name", b.Name, 3, 120) // nama 3–120 karakter
	if len([]rune(b.Description)) > 1000 {
		v.Add("description", "maksimal 1000 karakter")
	}
	if len([]rune(b.Specifications)) > 1000 {
		v.Add("specifications", "maksimal 1000 karakter")
	}
	v.Regex("category", b.Category, categoryRe, "kategori harus [a-z0-9-] 2–30 karakter")
	in := model.ProductInput{Name: b.Name, Description: b.Description, Specifications: b.Specifications,
		Category: b.Category, IsActive: true} // default aktif
	if b.Price == nil { // harga wajib
		v.Add("price", "wajib diisi")
	} else {
		v.Int("price", *b.Price, 0, maxPrice)
		in.Price = *b.Price
	}
	if create { // stok hanya relevan saat create
		if b.Stock == nil {
			v.Add("stock", "wajib diisi")
		} else {
			v.Int("stock", *b.Stock, 0, maxStock)
			in.Stock = *b.Stock
		}
	}
	if b.ImageURL != nil && *b.ImageURL != "" { // validasi URL gambar
		u, err := url.Parse(*b.ImageURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			v.Add("image_url", "harus URL http(s) yang valid")
		} else {
			in.ImageURL = b.ImageURL
		}
	}
	if b.IsActive != nil { // override status aktif
		in.IsActive = *b.IsActive
	}
	if e := v.Err(); e != nil {
		return in, e
	}
	return in, nil
}

// embedAfterWrite: embedding sinkron dengan timeout; gagal tidak menggagalkan request (worker akan mengulang).
func (d *Deps) embedAfterWrite(parent context.Context, p *model.Product) {
	if !d.Index.Enabled() { // embedding nonaktif
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second) // batasi 5 detik
	defer cancel()                                            // bebaskan timer
	if err := d.Index.IndexProduct(ctx, *p); err != nil {     // embed produk
		slog.Warn("embedding produk gagal; worker akan mengulang", "product_id", p.ID, "err", err)
	}
}

// createProduct: POST /api/products — buat produk baru (ADMIN_SISTEM).
func (d *Deps) createProduct(w http.ResponseWriter, r *http.Request) {
	var b productBody
	if err := h.ReadJSON(w, r, &b); err != nil { // baca body
		h.Fail(w, r, err)
		return
	}
	in, err := b.validate(true) // validasi
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p, err := d.Repo.Create(r.Context(), in, h.IdentityFrom(r.Context()).UserID) // simpan
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	d.embedAfterWrite(r.Context(), p)     // buat embedding
	h.WriteData(w, http.StatusCreated, p) // 201
}

// updateProduct: PUT /api/products/{id} — ubah produk (ADMIN_SISTEM).
func (d *Deps) updateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := productID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b productBody
	if err := h.ReadJSON(w, r, &b); err != nil { // baca body
		h.Fail(w, r, err)
		return
	}
	in, err := b.validate(false) // validasi (tanpa stok)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p, err := d.Repo.Update(r.Context(), id, in) // simpan
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	d.embedAfterWrite(r.Context(), p) // dilewati otomatis bila hash teks sama
	h.WriteData(w, http.StatusOK, p)
}

// deleteProduct: DELETE /api/products/{id} — nonaktifkan produk (soft delete, ADMIN_SISTEM).
func (d *Deps) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := productID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if err := d.Repo.SoftDelete(r.Context(), id); err != nil { // nonaktifkan
		h.Fail(w, r, err)
		return
	}
	h.WriteJSON(w, http.StatusNoContent, nil) // 204 tanpa body
}
