package handlers // handler HTTP pencarian semantik

import ( // package yang dipakai
	"errors"       // errors.Is
	"math"         // pembulatan
	"net/http"     // tipe HTTP
	"strconv"      // konversi string ke angka
	"strings"      // utilitas string
	"unicode/utf8" // hitung karakter

	"toko-ai-microservices/internal/shared/embedding"         // ErrUnavailable
	h "toko-ai-microservices/internal/shared/httputil"        // helper HTTP bersama
	"toko-ai-microservices/service-catalog/internal/model"    // tipe data
	"toko-ai-microservices/service-catalog/internal/semantic" // indexer
)

// round4 membulatkan angka ke 4 desimal.
func round4(f float64) float64 { return math.Round(f*10000) / 10000 }

// semanticSearch: GET /api/products/semantic-search (TRD 02 §6.2).
func (d *Deps) semanticSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))        // kalimat pencarian
	if n := utf8.RuneCountInString(q); n < 3 || n > 300 { // panjang harus 3–300
		h.Fail(w, r, h.ErrValidation("Parameter q wajib 3–300 karakter.",
			[]h.FieldError{{Field: "q", Message: "panjang harus 3–300 karakter"}}))
		return
	}
	limit, err := h.QueryInt(r, "limit", 5, 1, 20) // jumlah hasil
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	minScore := d.MinScore                            // ambang skor default
	if s := r.URL.Query().Get("min_score"); s != "" { // override ambang skor
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v < 0 || v > 1 {
			h.Fail(w, r, h.ErrValidation("Parameter min_score harus 0–1.",
				[]h.FieldError{{Field: "min_score", Message: "harus bilangan 0–1"}}))
			return
		}
		minScore = v
	}
	f, err := parseFilter(r) // filter kategori/harga/stok
	if err != nil {
		h.Fail(w, r, err)
		return
	}

	ctx := r.Context()
	active, err := d.Repo.ActiveProductsMap(ctx) // hanya produk aktif, harga & stok segar
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	keep := func(id string) bool { // penyaring kandidat sebelum diskor
		p, ok := active[id]
		if !ok { // bukan produk aktif
			return false
		}
		if f.Category != "" && p.Category != f.Category {
			return false
		}
		if f.MinPrice != nil && p.Price < *f.MinPrice {
			return false
		}
		if f.MaxPrice != nil && p.Price > *f.MaxPrice {
			return false
		}
		if f.InStock != nil && *f.InStock && p.Stock <= 0 {
			return false
		}
		return true
	}

	mode := "semantic"                                                    // mode pencarian
	results := []model.ScoredProduct{}                                    // hasil non-nil
	scored, err := d.Index.Search(ctx, q, limit, float32(minScore), keep) // pencarian vektor
	switch {
	case err == nil: // sukses
		for _, s := range scored {
			sc := round4(float64(s.Score)) // bulatkan skor
			results = append(results, model.ScoredProduct{Product: active[s.ID], Score: &sc})
		}
	case errors.Is(err, semantic.ErrFallback), errors.Is(err, embedding.ErrUnavailable): // embedding tidak tersedia
		mode = "keyword_fallback"
		w.Header().Set("x-search-mode", mode)
		items, kerr := d.Repo.KeywordSearch(ctx, q, f, limit) // pencarian keyword
		if kerr != nil {
			h.Fail(w, r, kerr)
			return
		}
		for _, p := range items {
			results = append(results, model.ScoredProduct{Product: p, Score: nil}) // tanpa skor
		}
	default: // error lain
		h.Fail(w, r, err)
		return
	}
	h.WriteList(w, results, h.Meta{"total": len(results), "limit": limit, "query": q, "mode": mode, "min_score": minScore})
}

// reindex: POST /api/products/reindex (ADMIN_SISTEM) -> 202 {"data":{"queued":N}}.
func (d *Deps) reindex(w http.ResponseWriter, r *http.Request) {
	n, err := d.Index.Reindex(r.Context()) // mulai reindex di background
	switch {
	case err == nil: // diterima
		h.WriteData(w, http.StatusAccepted, map[string]int{"queued": n})
	case errors.Is(err, semantic.ErrBusy): // sedang berjalan
		h.Fail(w, r, h.ErrConflict("CONFLICT", "Reindex sedang berjalan.", nil))
	case errors.Is(err, embedding.ErrUnavailable): // embedding mati
		h.Fail(w, r, h.NewError(http.StatusServiceUnavailable, "EMBEDDING_UNAVAILABLE", "Embedding tidak tersedia (GOOGLE_API_KEY kosong).", nil))
	default:
		h.Fail(w, r, err)
	}
}
