package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"time"

	h "toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/validate"
	"toko-ai-microservices/service-catalog/internal/model"
)

var (
	idRe       = regexp.MustCompile(`^PRD-\d{3,}$`)
	categoryRe = regexp.MustCompile(`^[a-z0-9-]{2,30}$`)
)

const (
	maxPrice = 1_000_000_000
	maxStock = 1_000_000
)

func isAdmin(r *http.Request) bool { return h.IdentityFrom(r.Context()).IsAdmin() }

// productID memvalidasi {id}; format salah diperlakukan sebagai tidak ditemukan.
func productID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !idRe.MatchString(id) {
		return "", h.ErrNotFound("Produk " + id + " tidak ditemukan.")
	}
	return id, nil
}

// parseFilter membaca query filter bersama (eksak & semantik).
func parseFilter(r *http.Request) (model.ListFilter, error) {
	q := r.URL.Query()
	f := model.ListFilter{Q: q.Get("q"), Category: q.Get("category"), Sort: q.Get("sort")}
	v := validate.New()
	if f.Category != "" && !categoryRe.MatchString(f.Category) {
		v.Add("category", "format kategori tidak valid")
	}
	if f.Sort != "" {
		v.OneOf("sort", f.Sort, "name", "price_asc", "price_desc", "newest")
	}
	var err error
	if q.Get("min_price") != "" {
		n, e := h.QueryInt(r, "min_price", 0, 0, maxPrice)
		if e != nil {
			return f, e
		}
		f.MinPrice = &n
	}
	if q.Get("max_price") != "" {
		n, e := h.QueryInt(r, "max_price", 0, 0, maxPrice)
		if e != nil {
			return f, e
		}
		f.MaxPrice = &n
	}
	if f.InStock, err = h.QueryBool(r, "in_stock"); err != nil {
		return f, err
	}
	if e := v.Err(); e != nil {
		return f, e
	}
	return f, nil
}

func (d *Deps) listProducts(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if f.Page, f.Limit, err = h.Pagination(r); err != nil {
		h.Fail(w, r, err)
		return
	}
	inc, err := h.QueryBool(r, "include_inactive")
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	f.IncludeInactive = inc != nil && *inc && isAdmin(r) // role lain diabaikan
	items, total, err := d.Repo.List(r.Context(), f)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteList(w, items, h.PageMeta(total, f.Page, f.Limit))
}

func (d *Deps) categories(w http.ResponseWriter, r *http.Request) {
	cats, err := d.Repo.Categories(r.Context())
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, cats)
}

func (d *Deps) getProduct(w http.ResponseWriter, r *http.Request) {
	id, err := productID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p, err := d.Repo.Get(r.Context(), id)
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

type productBody struct {
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	Specifications string  `json:"specifications"`
	Category       string  `json:"category"`
	Price          *int    `json:"price"`
	Stock          *int    `json:"stock"`
	ImageURL       *string `json:"image_url"`
	IsActive       *bool   `json:"is_active"`
}

func (b productBody) validate(create bool) (model.ProductInput, error) {
	v := validate.New()
	v.Len("name", b.Name, 3, 120)
	if len([]rune(b.Description)) > 1000 {
		v.Add("description", "maksimal 1000 karakter")
	}
	if len([]rune(b.Specifications)) > 1000 {
		v.Add("specifications", "maksimal 1000 karakter")
	}
	v.Regex("category", b.Category, categoryRe, "kategori harus [a-z0-9-] 2–30 karakter")
	in := model.ProductInput{Name: b.Name, Description: b.Description, Specifications: b.Specifications,
		Category: b.Category, IsActive: true}
	if b.Price == nil {
		v.Add("price", "wajib diisi")
	} else {
		v.Int("price", *b.Price, 0, maxPrice)
		in.Price = *b.Price
	}
	if create {
		if b.Stock == nil {
			v.Add("stock", "wajib diisi")
		} else {
			v.Int("stock", *b.Stock, 0, maxStock)
			in.Stock = *b.Stock
		}
	}
	if b.ImageURL != nil && *b.ImageURL != "" {
		u, err := url.Parse(*b.ImageURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			v.Add("image_url", "harus URL http(s) yang valid")
		} else {
			in.ImageURL = b.ImageURL
		}
	}
	if b.IsActive != nil {
		in.IsActive = *b.IsActive
	}
	if e := v.Err(); e != nil {
		return in, e
	}
	return in, nil
}

// embedAfterWrite: embedding sinkron dengan timeout; gagal tidak menggagalkan request (worker akan mengulang).
func (d *Deps) embedAfterWrite(parent context.Context, p *model.Product) {
	if !d.Index.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	if err := d.Index.IndexProduct(ctx, *p); err != nil {
		slog.Warn("embedding produk gagal; worker akan mengulang", "product_id", p.ID, "err", err)
	}
}

func (d *Deps) createProduct(w http.ResponseWriter, r *http.Request) {
	var b productBody
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	in, err := b.validate(true)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p, err := d.Repo.Create(r.Context(), in, h.IdentityFrom(r.Context()).UserID)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	d.embedAfterWrite(r.Context(), p)
	h.WriteData(w, http.StatusCreated, p)
}

func (d *Deps) updateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := productID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b productBody
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	in, err := b.validate(false)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p, err := d.Repo.Update(r.Context(), id, in)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	d.embedAfterWrite(r.Context(), p) // dilewati otomatis bila hash teks sama
	h.WriteData(w, http.StatusOK, p)
}

func (d *Deps) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := productID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if err := d.Repo.SoftDelete(r.Context(), id); err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteJSON(w, http.StatusNoContent, nil)
}
