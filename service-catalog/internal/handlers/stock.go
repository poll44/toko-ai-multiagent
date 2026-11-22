package handlers

import (
	"net/http"
	"regexp"
	"strconv"

	h "toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/validate"
	"toko-ai-microservices/service-catalog/internal/model"
)

func (d *Deps) adjustStock(w http.ResponseWriter, r *http.Request) {
	id, err := productID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct {
		Mode  string `json:"mode"`
		Value *int   `json:"value"`
		Note  string `json:"note"`
	}
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	v.OneOf("mode", b.Mode, "set", "adjust")
	if b.Value == nil {
		v.Add("value", "wajib diisi")
	} else {
		v.Int("value", *b.Value, -maxStock, maxStock)
		if b.Mode == "set" && *b.Value < 0 {
			v.Add("value", "mode set tidak boleh negatif")
		}
	}
	if len([]rune(b.Note)) > 200 {
		v.Add("note", "maksimal 200 karakter")
	}
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	p, err := d.Repo.AdjustStock(r.Context(), id, b.Mode, *b.Value, h.IdentityFrom(r.Context()).UserID)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, p)
}

func (d *Deps) stockMovements(w http.ResponseWriter, r *http.Request) {
	id, err := productID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	ms, err := d.Repo.Movements(r.Context(), id)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, ms)
}

// ---- endpoint internal (hanya service-order) ----

type stockBody struct {
	ReferenceID string            `json:"reference_id"`
	Reason      string            `json:"reason"`
	Items       []model.StockItem `json:"items"`
}

var refRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

func (b stockBody) validate(needReason bool) error {
	v := validate.New()
	v.Regex("reference_id", b.ReferenceID, refRe, "reference_id tidak valid")
	if needReason {
		v.Len("reason", b.Reason, 1, 50)
	}
	if n := len(b.Items); n < 1 || n > 20 {
		v.Add("items", "harus 1–20 item")
	}
	seen := map[string]bool{}
	for i, it := range b.Items {
		if !idRe.MatchString(it.ProductID) {
			v.Add("items["+strconv.Itoa(i)+"].product_id", "format produk tidak valid")
		}
		if seen[it.ProductID] {
			v.Add("items["+strconv.Itoa(i)+"].product_id", "duplikat")
		}
		seen[it.ProductID] = true
		if it.Quantity < 1 || it.Quantity > 99 {
			v.Add("items["+strconv.Itoa(i)+"].quantity", "harus 1–99")
		}
	}
	if e := v.Err(); e != nil {
		return e
	}
	return nil
}

func (d *Deps) decrement(w http.ResponseWriter, r *http.Request) {
	var b stockBody
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	if err := b.validate(false); err != nil {
		h.Fail(w, r, err)
		return
	}
	items, err := d.Repo.Decrement(r.Context(), b.ReferenceID, b.Items, h.IdentityFrom(r.Context()).UserID)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, map[string]any{"reference_id": b.ReferenceID, "items": items})
}

func (d *Deps) restock(w http.ResponseWriter, r *http.Request) {
	var b stockBody
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	if err := b.validate(true); err != nil {
		h.Fail(w, r, err)
		return
	}
	applied, err := d.Repo.Restock(r.Context(), b.ReferenceID, b.Items, h.IdentityFrom(r.Context()).UserID)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, map[string]any{"reference_id": b.ReferenceID, "restocked": applied})
}
