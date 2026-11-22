package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"toko-ai-microservices/internal/shared/embedding"
	h "toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/validate"
	"toko-ai-microservices/service-ticket/internal/knowledge"
	"toko-ai-microservices/service-ticket/internal/model"
)

func docID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !docIDRe.MatchString(id) {
		return "", h.ErrNotFound("Dokumen " + id + " tidak ditemukan.")
	}
	return id, nil
}

// searchKnowledge: GET /api/knowledge/search (PRD 04 §7.1) — terbuka untuk GUEST.
func (d *Deps) searchKnowledge(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 3 || n > 300 {
		h.Fail(w, r, h.ErrValidation("Parameter q wajib 3–300 karakter.", []h.FieldError{{Field: "q", Message: "panjang harus 3–300 karakter"}}))
		return
	}
	limit, err := h.QueryInt(r, "limit", 3, 1, 5)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	category := r.URL.Query().Get("category")
	if category != "" {
		v := validate.New()
		v.OneOf("category", category, model.KBCategories...)
		if e := v.Err(); e != nil {
			h.Fail(w, r, e)
			return
		}
	}
	minScore := d.MinScore
	if s := r.URL.Query().Get("min_score"); s != "" {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f < 0 || f > 1 {
			h.Fail(w, r, h.ErrValidation("Parameter min_score harus 0–1.", []h.FieldError{{Field: "min_score", Message: "harus bilangan 0–1"}}))
			return
		}
		minScore = f
	}
	results, mode, err := d.Index.Search(r.Context(), q, limit, float32(minScore), category)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if mode == "keyword_fallback" {
		w.Header().Set("x-search-mode", mode)
	}
	h.WriteList(w, results, h.Meta{"total": len(results), "limit": limit, "query": q, "mode": mode, "min_score": minScore})
}

func (d *Deps) listDocs(w http.ResponseWriter, r *http.Request) {
	inc, err := h.QueryBool(r, "include_inactive")
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

func (d *Deps) getDoc(w http.ResponseWriter, r *http.Request) {
	id, err := docID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	doc, err := d.Repo.GetDoc(r.Context(), id)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if !doc.IsActive && !ident(r).IsAdmin() {
		h.Fail(w, r, h.ErrNotFound("Dokumen "+id+" tidak ditemukan."))
		return
	}
	h.WriteData(w, http.StatusOK, doc)
}

type docBody struct {
	Title    string `json:"title"`
	Category string `json:"category"`
	Content  string `json:"content"`
	IsActive *bool  `json:"is_active"`
}

func (b docBody) validate() (model.DocumentInput, error) {
	v := validate.New()
	v.Len("title", b.Title, 5, 120)
	v.OneOf("category", b.Category, model.KBCategories...)
	v.Len("content", b.Content, 20, 6000)
	in := model.DocumentInput{Title: strings.TrimSpace(b.Title), Category: b.Category, Content: strings.TrimSpace(b.Content), IsActive: true}
	if b.IsActive != nil {
		in.IsActive = *b.IsActive
	}
	if e := v.Err(); e != nil {
		return in, e
	}
	return in, nil
}

// embedAfterWrite: embedding sinkron dengan timeout; gagal tidak menggagalkan request (worker akan mengulang).
func (d *Deps) embedAfterWrite(parent context.Context) {
	if !d.Index.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	if _, err := d.Index.IndexPending(ctx, false, 0); err != nil {
		slog.Warn("embedding knowledge gagal; worker akan mengulang", "err", err)
	}
}

func (d *Deps) createDoc(w http.ResponseWriter, r *http.Request) {
	var b docBody
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	in, err := b.validate()
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	id, err := d.Repo.CreateDoc(r.Context(), in, knowledge.Split(in.Content), knowledge.Hash)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	d.embedAfterWrite(r.Context())
	doc, err := d.Repo.GetDoc(r.Context(), id)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusCreated, doc)
}

func (d *Deps) updateDoc(w http.ResponseWriter, r *http.Request) {
	id, err := docID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b docBody
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	in, err := b.validate()
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if err := d.Repo.UpdateDoc(r.Context(), id, in, knowledge.Split(in.Content), knowledge.Hash); err != nil {
		h.Fail(w, r, err)
		return
	}
	d.embedAfterWrite(r.Context())
	doc, err := d.Repo.GetDoc(r.Context(), id)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, doc)
}

func (d *Deps) deleteDoc(w http.ResponseWriter, r *http.Request) {
	id, err := docID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if err := d.Repo.SoftDeleteDoc(r.Context(), id); err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteJSON(w, http.StatusNoContent, nil)
}

func (d *Deps) reindex(w http.ResponseWriter, r *http.Request) {
	n, err := d.Index.Reindex(r.Context())
	switch {
	case err == nil:
		h.WriteData(w, http.StatusAccepted, map[string]int{"queued": n})
	case errors.Is(err, knowledge.ErrBusy):
		h.Fail(w, r, h.ErrConflict("CONFLICT", "Reindex sedang berjalan.", nil))
	case errors.Is(err, embedding.ErrUnavailable):
		h.Fail(w, r, h.NewError(http.StatusServiceUnavailable, "EMBEDDING_UNAVAILABLE", "Embedding tidak tersedia (GOOGLE_API_KEY kosong).", nil))
	default:
		h.Fail(w, r, err)
	}
}
