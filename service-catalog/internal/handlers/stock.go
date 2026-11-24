package handlers // handler HTTP untuk stok

import ( // package yang dipakai
	"net/http" // tipe HTTP
	"regexp"   // regular expression
	"strconv"  // konversi angka ke string

	h "toko-ai-microservices/internal/shared/httputil"     // helper HTTP bersama
	"toko-ai-microservices/internal/shared/validate"       // validator
	"toko-ai-microservices/service-catalog/internal/model" // tipe data
)

// adjustStock: PATCH /api/products/{id}/stock — admin menyesuaikan stok (mode set/adjust).
func (d *Deps) adjustStock(w http.ResponseWriter, r *http.Request) {
	id, err := productID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct { // payload
		Mode  string `json:"mode"`  // "set" atau "adjust"
		Value *int   `json:"value"` // nilai stok / selisih
		Note  string `json:"note"`  // catatan (opsional)
	}
	if err := h.ReadJSON(w, r, &b); err != nil { // baca body
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	v.OneOf("mode", b.Mode, "set", "adjust") // mode harus valid
	if b.Value == nil {                      // value wajib
		v.Add("value", "wajib diisi")
	} else {
		v.Int("value", *b.Value, -maxStock, maxStock)
		if b.Mode == "set" && *b.Value < 0 { // mode set tidak boleh negatif
			v.Add("value", "mode set tidak boleh negatif")
		}
	}
	if len([]rune(b.Note)) > 200 { // batasi panjang catatan
		v.Add("note", "maksimal 200 karakter")
	}
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	p, err := d.Repo.AdjustStock(r.Context(), id, b.Mode, *b.Value, h.IdentityFrom(r.Context()).UserID) // ubah stok
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, p)
}

// stockMovements: GET /api/products/{id}/stock-movements — riwayat perubahan stok.
func (d *Deps) stockMovements(w http.ResponseWriter, r *http.Request) {
	id, err := productID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	ms, err := d.Repo.Movements(r.Context(), id) // ambil riwayat
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, ms)
}

// ---- endpoint internal (hanya service-order) ----

// stockBody: payload untuk decrement/restock.
type stockBody struct {
	ReferenceID string            `json:"reference_id"` // referensi (mis. ID pesanan)
	Reason      string            `json:"reason"`       // alasan (wajib untuk restock)
	Items       []model.StockItem `json:"items"`        // daftar item
}

// refRe: format reference_id yang diizinkan.
var refRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

// validate memvalidasi payload stok; needReason=true bila alasan wajib.
func (b stockBody) validate(needReason bool) error {
	v := validate.New()
	v.Regex("reference_id", b.ReferenceID, refRe, "reference_id tidak valid")
	if needReason { // alasan wajib (restock)
		v.Len("reason", b.Reason, 1, 50)
	}
	if n := len(b.Items); n < 1 || n > 20 { // 1–20 item
		v.Add("items", "harus 1–20 item")
	}
	seen := map[string]bool{} // untuk deteksi duplikat
	for i, it := range b.Items {
		if !idRe.MatchString(it.ProductID) { // format ID produk
			v.Add("items["+strconv.Itoa(i)+"].product_id", "format produk tidak valid")
		}
		if seen[it.ProductID] { // duplikat
			v.Add("items["+strconv.Itoa(i)+"].product_id", "duplikat")
		}
		seen[it.ProductID] = true
		if it.Quantity < 1 || it.Quantity > 99 { // batas jumlah
			v.Add("items["+strconv.Itoa(i)+"].quantity", "harus 1–99")
		}
	}
	if e := v.Err(); e != nil {
		return e
	}
	return nil
}

// decrement: POST /internal/stock/decrement — kurangi stok semua item secara atomik.
func (d *Deps) decrement(w http.ResponseWriter, r *http.Request) {
	var b stockBody
	if err := h.ReadJSON(w, r, &b); err != nil { // baca body
		h.Fail(w, r, err)
		return
	}
	if err := b.validate(false); err != nil { // validasi
		h.Fail(w, r, err)
		return
	}
	items, err := d.Repo.Decrement(r.Context(), b.ReferenceID, b.Items, h.IdentityFrom(r.Context()).UserID) // kurangi stok
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, map[string]any{"reference_id": b.ReferenceID, "items": items})
}

// restock: POST /internal/stock/restock — kembalikan stok (idempoten per reference_id).
func (d *Deps) restock(w http.ResponseWriter, r *http.Request) {
	var b stockBody
	if err := h.ReadJSON(w, r, &b); err != nil { // baca body
		h.Fail(w, r, err)
		return
	}
	if err := b.validate(true); err != nil { // validasi (alasan wajib)
		h.Fail(w, r, err)
		return
	}
	applied, err := d.Repo.Restock(r.Context(), b.ReferenceID, b.Items, h.IdentityFrom(r.Context()).UserID) // kembalikan stok
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, map[string]any{"reference_id": b.ReferenceID, "restocked": applied})
}
