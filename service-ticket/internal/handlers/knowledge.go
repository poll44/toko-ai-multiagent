package handlers // handler HTTP untuk knowledge base

import ( // package yang dipakai
	"context"      // context
	"errors"       // errors.Is
	"log/slog"     // logging terstruktur
	"net/http"     // tipe HTTP
	"strconv"      // konversi string ke angka
	"strings"      // utilitas string
	"time"         // timeout
	"unicode/utf8" // hitung karakter

	"toko-ai-microservices/internal/shared/embedding"         // ErrUnavailable
	h "toko-ai-microservices/internal/shared/httputil"        // helper HTTP bersama
	"toko-ai-microservices/internal/shared/validate"          // validator
	"toko-ai-microservices/service-ticket/internal/knowledge" // chunker & indexer
	"toko-ai-microservices/service-ticket/internal/model"     // tipe data
)

// docID memvalidasi {id}; format salah => 404.
func docID(r *http.Request) (string, error) {
	id := r.PathValue("id")       // ambil {id} dari path
	if !docIDRe.MatchString(id) { // format salah
		return "", h.ErrNotFound("Dokumen " + id + " tidak ditemukan.")
	}
	return id, nil
}

// searchKnowledge: GET /api/knowledge/search (TRD 04 §7.1) — terbuka untuk GUEST.
func (d *Deps) searchKnowledge(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))        // kalimat pencarian
	if n := utf8.RuneCountInString(q); n < 3 || n > 300 { // panjang harus 3–300
		h.Fail(w, r, h.ErrValidation("Parameter q wajib 3–300 karakter.", []h.FieldError{{Field: "q", Message: "panjang harus 3–300 karakter"}}))
		return
	}
	limit, err := h.QueryInt(r, "limit", 3, 1, 5) // jumlah hasil
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	category := r.URL.Query().Get("category") // filter kategori (opsional)
	if category != "" {
		v := validate.New()
		v.OneOf("category", category, model.KBCategories...) // kategori harus valid
		if e := v.Err(); e != nil {
			h.Fail(w, r, e)
			return
		}
	}
	minScore := d.MinScore                            // ambang skor default
	if s := r.URL.Query().Get("min_score"); s != "" { // override ambang skor
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f < 0 || f > 1 {
			h.Fail(w, r, h.ErrValidation("Parameter min_score harus 0–1.", []h.FieldError{{Field: "min_score", Message: "harus bilangan 0–1"}}))
			return
		}
		minScore = f
	}
	results, mode, err := d.Index.Search(r.Context(), q, limit, float32(minScore), category) // cari chunk relevan
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if mode == "keyword_fallback" { // beri tahu klien bahwa pencarian cerdas tidak dipakai
		w.Header().Set("x-search-mode", mode)
	}
	h.WriteList(w, results, h.Meta{"total": len(results), "limit": limit, "query": q, "mode": mode, "min_score": minScore})
}

// listDocs: GET /api/knowledge/documents — daftar dokumen (nonaktif hanya untuk admin).
func (d *Deps) listDocs(w http.ResponseWriter, r *http.Request) {
	inc, err := h.QueryBool(r, "include_inactive") // sertakan nonaktif?
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	docs, err := d.Repo.ListDocs(r.Context(), inc != nil && *inc && ident(r).IsAdmin()) // role lain diabaikan
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, docs)
}

// getDoc: GET /api/knowledge/documents/{id} — detail dokumen (nonaktif hanya untuk admin).
func (d *Deps) getDoc(w http.ResponseWriter, r *http.Request) {
	id, err := docID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	doc, err := d.Repo.GetDoc(r.Context(), id) // ambil dokumen
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if !doc.IsActive && !ident(r).IsAdmin() { // sembunyikan dokumen nonaktif
		h.Fail(w, r, h.ErrNotFound("Dokumen "+id+" tidak ditemukan."))
		return
	}
	h.WriteData(w, http.StatusOK, doc)
}

// docBody: payload JSON untuk membuat/mengubah dokumen.
type docBody struct {
	Title    string `json:"title"`     // judul
	Category string `json:"category"`  // kategori KB
	Content  string `json:"content"`   // isi dokumen
	IsActive *bool  `json:"is_active"` // status aktif (opsional)
}

// validate memvalidasi payload dan mengubahnya menjadi model.DocumentInput.
func (b docBody) validate() (model.DocumentInput, error) {
	v := validate.New()
	v.Len("title", b.Title, 5, 120)                                                                                                           // judul 5–120 karakter
	v.OneOf("category", b.Category, model.KBCategories...)                                                                                    // kategori valid
	v.Len("content", b.Content, 20, 6000)                                                                                                     // isi 20–6000 karakter
	in := model.DocumentInput{Title: strings.TrimSpace(b.Title), Category: b.Category, Content: strings.TrimSpace(b.Content), IsActive: true} // default aktif
	if b.IsActive != nil {                                                                                                                    // override status aktif
		in.IsActive = *b.IsActive
	}
	if e := v.Err(); e != nil {
		return in, e
	}
	return in, nil
}

// embedAfterWrite: embedding sinkron dengan timeout; gagal tidak menggagalkan request (worker akan mengulang).
func (d *Deps) embedAfterWrite(parent context.Context) {
	if !d.Index.Enabled() { // embedding nonaktif
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)      // batasi 5 detik
	defer cancel()                                                 // bebaskan timer
	if _, err := d.Index.IndexPending(ctx, false, 0); err != nil { // embed chunk yang belum terindeks
		slog.Warn("embedding knowledge gagal; worker akan mengulang", "err", err)
	}
}

// createDoc: POST /api/knowledge/documents — buat dokumen baru (ADMIN_SISTEM).
func (d *Deps) createDoc(w http.ResponseWriter, r *http.Request) {
	var b docBody
	if err := h.ReadJSON(w, r, &b); err != nil { // baca body
		h.Fail(w, r, err)
		return
	}
	in, err := b.validate() // validasi
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	id, err := d.Repo.CreateDoc(r.Context(), in, knowledge.Split(in.Content), knowledge.Hash) // simpan dokumen + chunk
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	d.embedAfterWrite(r.Context())             // buat embedding
	doc, err := d.Repo.GetDoc(r.Context(), id) // ambil dokumen lengkap
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusCreated, doc) // 201
}

// updateDoc: PUT /api/knowledge/documents/{id} — ubah dokumen (ADMIN_SISTEM).
func (d *Deps) updateDoc(w http.ResponseWriter, r *http.Request) {
	id, err := docID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b docBody
	if err := h.ReadJSON(w, r, &b); err != nil { // baca body
		h.Fail(w, r, err)
		return
	}
	in, err := b.validate() // validasi
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if err := d.Repo.UpdateDoc(r.Context(), id, in, knowledge.Split(in.Content), knowledge.Hash); err != nil { // simpan perubahan + chunk baru
		h.Fail(w, r, err)
		return
	}
	d.embedAfterWrite(r.Context())             // buat embedding ulang
	doc, err := d.Repo.GetDoc(r.Context(), id) // ambil dokumen lengkap
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, doc)
}

// deleteDoc: DELETE /api/knowledge/documents/{id} — nonaktifkan dokumen (soft delete, ADMIN_SISTEM).
func (d *Deps) deleteDoc(w http.ResponseWriter, r *http.Request) {
	id, err := docID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if err := d.Repo.SoftDeleteDoc(r.Context(), id); err != nil { // nonaktifkan
		h.Fail(w, r, err)
		return
	}
	h.WriteJSON(w, http.StatusNoContent, nil) // 204 tanpa body
}

// reindex: POST /api/knowledge/reindex (ADMIN_SISTEM) -> 202 {"data":{"queued":N}}.
func (d *Deps) reindex(w http.ResponseWriter, r *http.Request) {
	n, err := d.Index.Reindex(r.Context()) // mulai reindex di background
	switch {
	case err == nil: // diterima
		h.WriteData(w, http.StatusAccepted, map[string]int{"queued": n})
	case errors.Is(err, knowledge.ErrBusy): // sedang berjalan
		h.Fail(w, r, h.ErrConflict("CONFLICT", "Reindex sedang berjalan.", nil))
	case errors.Is(err, embedding.ErrUnavailable): // embedding mati
		h.Fail(w, r, h.NewError(http.StatusServiceUnavailable, "EMBEDDING_UNAVAILABLE", "Embedding tidak tersedia (GOOGLE_API_KEY kosong).", nil))
	default:
		h.Fail(w, r, err)
	}
}
