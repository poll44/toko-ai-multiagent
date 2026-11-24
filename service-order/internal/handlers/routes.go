// Package handlers: routing + guard RBAC + handler service-order (TRD 03 §2, §6).
package handlers // nama package: handlers

import ( // package yang dipakai
	"net/http" // tipe HTTP
	"regexp"   // regular expression
	"slices"   // slices.Contains

	h "toko-ai-microservices/internal/shared/httputil"     // helper HTTP bersama (alias h)
	"toko-ai-microservices/internal/shared/validate"       // validator
	"toko-ai-microservices/service-order/internal/model"   // tipe data
	"toko-ai-microservices/service-order/internal/service" // logika bisnis
)

// Deps: dependensi handler.
type Deps struct{ Svc *service.Service }

var (
	orderIDRe   = regexp.MustCompile(`^ORD-\d{3,}$`)           // format ID pesanan
	productIDRe = regexp.MustCompile(`^PRD-\d{3,}$`)           // format ID produk
	userIDRe    = regexp.MustCompile(`^(USR|ADM|DEV)-\d{2,}$`) // format ID user
)

// Register mendaftarkan semua rute service-order beserta guard RBAC-nya ke mux.
func Register(mux *http.ServeMux, internalKey string, d *Deps) {
	secured := h.Chain(h.InternalAuthMiddleware(internalKey), h.IdentityMiddleware(), h.DenyGuest) // kunci internal + identitas + tolak tamu
	customer := h.RequireRole(h.RoleCustomer)                                                      // hanya CUSTOMER
	custOrAdmin := h.RequireRole(h.RoleCustomer, h.RoleAdminSistem)                                // CUSTOMER atau ADMIN_SISTEM
	admin := h.RequireRole(h.RoleAdminSistem)                                                      // hanya ADMIN_SISTEM

	mux.HandleFunc("GET /health", h.Health("service-order")) // health check tanpa auth

	mux.Handle("POST /api/orders", secured(customer(http.HandlerFunc(d.create))))                // buat pesanan
	mux.Handle("GET /api/orders", secured(http.HandlerFunc(d.list)))                             // daftar pesanan
	mux.Handle("GET /api/orders/{id}", secured(http.HandlerFunc(d.get)))                         // detail pesanan
	mux.Handle("POST /api/orders/{id}/pay", secured(customer(http.HandlerFunc(d.pay))))          // bayar (simulasi)
	mux.Handle("POST /api/orders/{id}/cancel", secured(custOrAdmin(http.HandlerFunc(d.cancel)))) // batalkan
	mux.Handle("PATCH /api/orders/{id}/status", secured(admin(http.HandlerFunc(d.setStatus))))   // ubah status (admin)
}

// orderID memvalidasi {id}; format salah => 404.
func orderID(r *http.Request) (string, error) {
	id := r.PathValue("id")         // ambil {id} dari path
	if !orderIDRe.MatchString(id) { // format salah
		return "", h.ErrNotFound("Pesanan " + id + " tidak ditemukan.")
	}
	return id, nil
}

// ident mengambil identitas pemanggil dari context request.
func ident(r *http.Request) h.Identity { return h.IdentityFrom(r.Context()) }

// createBody: payload pembuatan pesanan.
type createBody struct {
	Items []struct { // daftar barang
		ProductID string `json:"product_id"` // ID produk
		Quantity  int    `json:"quantity"`   // jumlah
	} `json:"items"`
	PaymentMethod   string `json:"payment_method"`   // metode pembayaran
	ShippingAddress string `json:"shipping_address"` // alamat pengiriman
	// Field harga (mis. "price") di body klien sengaja TIDAK dibaca — ReadJSON mengabaikan field tak dikenal.
}

// create: POST /api/orders — validasi payload lalu buat pesanan.
func (d *Deps) create(w http.ResponseWriter, r *http.Request) {
	var b createBody
	if err := h.ReadJSON(w, r, &b); err != nil { // baca body
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	if n := len(b.Items); n < 1 || n > 20 { // 1–20 item
		v.Add("items", "harus 1–20 item")
	}
	seen := map[string]bool{} // deteksi duplikat
	in := service.CreateInput{PaymentMethod: b.PaymentMethod, ShippingAddress: b.ShippingAddress}
	for i, it := range b.Items {
		f := "items[" + itoa(i) + "]" // nama field untuk pesan error
		if !productIDRe.MatchString(it.ProductID) {
			v.Add(f+".product_id", "format produk tidak valid")
		}
		if seen[it.ProductID] {
			v.Add(f+".product_id", "duplikat")
		}
		seen[it.ProductID] = true
		if it.Quantity < 1 || it.Quantity > 99 { // batas jumlah
			v.Add(f+".quantity", "harus 1–99")
		}
		in.Items = append(in.Items, service.ItemInput{ProductID: it.ProductID, Quantity: it.Quantity})
	}
	v.OneOf("payment_method", b.PaymentMethod, "EWALLET", "BANK_TRANSFER", "CREDIT_CARD") // metode valid
	v.Len("shipping_address", b.ShippingAddress, 10, 300)                                 // alamat 10–300 karakter
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	o, err := d.Svc.Create(r.Context(), ident(r), in) // buat pesanan
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusCreated, o) // 201
}

// itoa mengubah int non-negatif menjadi string desimal.
func itoa(i int) string {
	if i < 10 { // satu digit
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10)) // rekursif untuk digit lebih
}

// list: GET /api/orders — CUSTOMER hanya data sendiri; admin boleh melihat semua/filter user.
func (d *Deps) list(w http.ResponseWriter, r *http.Request) {
	id := ident(r)                      // identitas pemanggil
	page, limit, err := h.Pagination(r) // baca paginasi
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	f := model.ListFilter{Status: r.URL.Query().Get("status"), Page: page, Limit: limit}
	if f.Status != "" && !slices.Contains(model.Statuses, f.Status) { // status harus valid
		h.Fail(w, r, h.ErrValidation("Parameter status tidak valid.", []h.FieldError{{Field: "status", Message: "harus salah satu dari: " + joinStatuses()}}))
		return
	}
	if id.Role == h.RoleCustomer {
		f.UserID = &id.UserID // CUSTOMER dipaksa ke data sendiri; ?user_id= diabaikan
	} else if u := r.URL.Query().Get("user_id"); u != "" { // admin: filter user opsional
		if !userIDRe.MatchString(u) {
			h.Fail(w, r, h.ErrValidation("Parameter user_id tidak valid.", []h.FieldError{{Field: "user_id", Message: "format tidak valid"}}))
			return
		}
		f.UserID = &u
	}
	orders, total, err := d.Svc.List(r.Context(), f) // ambil data
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteList(w, orders, h.PageMeta(total, page, limit))
}

// joinStatuses menggabungkan semua status valid menjadi "A, B, C" untuk pesan error.
func joinStatuses() string {
	s := ""
	for i, st := range model.Statuses {
		if i > 0 { // pemisah antar status
			s += ", "
		}
		s += st
	}
	return s
}

// get: GET /api/orders/{id} — detail pesanan.
func (d *Deps) get(w http.ResponseWriter, r *http.Request) {
	oid, err := orderID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	o, err := d.Svc.Get(r.Context(), ident(r), oid) // ambil pesanan
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, o)
}

// pay: POST /api/orders/{id}/pay — simulasi pembayaran (success|fail|timeout).
func (d *Deps) pay(w http.ResponseWriter, r *http.Request) {
	oid, err := orderID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct { // payload opsional
		Simulate   string `json:"simulate"`    // hasil simulasi
		FailReason string `json:"fail_reason"` // alasan gagal kustom
	}
	if err := h.ReadJSONOptional(w, r, &b); err != nil { // body boleh kosong
		h.Fail(w, r, err)
		return
	}
	if b.Simulate == "" { // default: sukses
		b.Simulate = "success"
	}
	v := validate.New()
	v.OneOf("simulate", b.Simulate, "success", "fail", "timeout")
	if len([]rune(b.FailReason)) > 200 {
		v.Add("fail_reason", "maksimal 200 karakter")
	}
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	o, err := d.Svc.Pay(r.Context(), ident(r), oid, b.Simulate, b.FailReason) // proses pembayaran
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, o)
}

// cancel: POST /api/orders/{id}/cancel — batalkan pesanan.
func (d *Deps) cancel(w http.ResponseWriter, r *http.Request) {
	oid, err := orderID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct { // payload opsional
		Reason string `json:"reason"` // alasan pembatalan
	}
	if err := h.ReadJSONOptional(w, r, &b); err != nil { // body boleh kosong
		h.Fail(w, r, err)
		return
	}
	if len([]rune(b.Reason)) > 200 { // batasi panjang alasan
		h.Fail(w, r, h.ErrValidation("Alasan maksimal 200 karakter.", []h.FieldError{{Field: "reason", Message: "maksimal 200 karakter"}}))
		return
	}
	o, err := d.Svc.Cancel(r.Context(), ident(r), oid, b.Reason) // proses pembatalan
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, o)
}

// setStatus: PATCH /api/orders/{id}/status — admin mengubah status pemenuhan pesanan.
func (d *Deps) setStatus(w http.ResponseWriter, r *http.Request) {
	oid, err := orderID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct { // payload
		Status         string `json:"status"`          // status tujuan
		TrackingNumber string `json:"tracking_number"` // nomor resi (wajib untuk SHIPPED)
		Note           string `json:"note"`            // catatan
	}
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	v.OneOf("status", b.Status, model.StatusProcessing, model.StatusShipped, model.StatusDelivered)
	if b.Status == model.StatusShipped { // resi wajib saat dikirim
		v.Len("tracking_number", b.TrackingNumber, 5, 40)
	}
	if len([]rune(b.Note)) > 200 {
		v.Add("note", "maksimal 200 karakter")
	}
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	o, err := d.Svc.SetStatus(r.Context(), ident(r), oid, b.Status, b.TrackingNumber, b.Note) // ubah status
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, o)
}
