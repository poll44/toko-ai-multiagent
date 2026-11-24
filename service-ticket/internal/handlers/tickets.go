package handlers // handler HTTP untuk tiket

import ( // package yang dipakai
	"net/http" // tipe HTTP

	h "toko-ai-microservices/internal/shared/httputil"      // helper HTTP bersama
	"toko-ai-microservices/internal/shared/validate"        // validator
	"toko-ai-microservices/service-ticket/internal/model"   // tipe data
	"toko-ai-microservices/service-ticket/internal/service" // logika bisnis
)

// ticketID memvalidasi {id}; format salah => 404.
func ticketID(r *http.Request) (string, error) {
	id := r.PathValue("id")          // ambil {id} dari path
	if !ticketIDRe.MatchString(id) { // format salah
		return "", h.ErrNotFound("Tiket " + id + " tidak ditemukan.")
	}
	return id, nil
}

// createTicket: POST /api/tickets — pelanggan membuat tiket komplain.
func (d *Deps) createTicket(w http.ResponseWriter, r *http.Request) {
	var b struct { // payload
		Subject     string `json:"subject"`     // subjek
		Description string `json:"description"` // deskripsi
		Category    string `json:"category"`    // kategori
		Priority    string `json:"priority"`    // prioritas (opsional)
		OrderID     string `json:"order_id"`    // pesanan terkait (opsional)
	}
	if err := h.ReadJSON(w, r, &b); err != nil { // baca body
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	v.Len("subject", b.Subject, 5, 120)                  // subjek 5–120 karakter
	v.Len("description", b.Description, 10, 2000)        // deskripsi 10–2000 karakter
	v.OneOf("category", b.Category, model.Categories...) // kategori valid
	if b.Priority != "" {                                // prioritas opsional
		v.OneOf("priority", b.Priority, model.Priorities...)
	}
	if b.OrderID != "" { // order_id opsional
		v.Regex("order_id", b.OrderID, orderIDRe, "format order_id tidak valid")
	}
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	t, err := d.Svc.Create(r.Context(), ident(r), service.CreateInput{ // buat tiket
		Subject: b.Subject, Description: b.Description, Category: b.Category, Priority: b.Priority, OrderID: b.OrderID,
	})
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusCreated, t) // 201
}

// listTickets: GET /api/tickets — CUSTOMER hanya data sendiri; admin boleh filter user/penanggung jawab.
func (d *Deps) listTickets(w http.ResponseWriter, r *http.Request) {
	id := ident(r)                      // identitas pemanggil
	page, limit, err := h.Pagination(r) // baca paginasi
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	q := r.URL.Query() // semua query param
	f := model.TicketFilter{Status: q.Get("status"), Category: q.Get("category"), Priority: q.Get("priority"), OrderID: q.Get("order_id"), Page: page, Limit: limit}
	v := validate.New()
	if f.Status != "" { // validasi filter status
		v.OneOf("status", f.Status, model.Statuses...)
	}
	if f.Category != "" { // validasi filter kategori
		v.OneOf("category", f.Category, model.Categories...)
	}
	if f.Priority != "" { // validasi filter prioritas
		v.OneOf("priority", f.Priority, model.Priorities...)
	}
	if f.OrderID != "" { // validasi filter pesanan
		v.Regex("order_id", f.OrderID, orderIDRe, "format order_id tidak valid")
	}
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	if id.Role == h.RoleCustomer {
		f.UserID = &id.UserID // dipaksa ke data sendiri; ?user_id= & ?assigned_to= diabaikan
	} else {
		if u := q.Get("user_id"); u != "" { // admin: filter pemilik
			if !userIDRe.MatchString(u) {
				h.Fail(w, r, h.ErrValidation("Parameter user_id tidak valid.", []h.FieldError{{Field: "user_id", Message: "format tidak valid"}}))
				return
			}
			f.UserID = &u
		}
		switch a := q.Get("assigned_to"); { // admin: filter penanggung jawab
		case a == "me": // tiket yang ditangani pemanggil
			f.AssignedTo = id.UserID
		case a != "":
			if !userIDRe.MatchString(a) {
				h.Fail(w, r, h.ErrValidation("Parameter assigned_to tidak valid.", []h.FieldError{{Field: "assigned_to", Message: "format tidak valid"}}))
				return
			}
			f.AssignedTo = a
		}
	}
	items, total, err := d.Svc.List(r.Context(), f) // ambil data
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteList(w, items, h.PageMeta(total, page, limit))
}

// getTicket: GET /api/tickets/{id} — detail tiket beserta pesan & riwayat.
func (d *Deps) getTicket(w http.ResponseWriter, r *http.Request) {
	tid, err := ticketID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	t, err := d.Svc.Get(r.Context(), ident(r), tid) // ambil tiket
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, t)
}

// addMessage: POST /api/tickets/{id}/messages — tambah pesan pada tiket.
func (d *Deps) addMessage(w http.ResponseWriter, r *http.Request) {
	tid, err := ticketID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct { // payload
		Message string `json:"message"` // isi pesan
	}
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	v.Len("message", b.Message, 1, 2000) // pesan 1–2000 karakter
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	m, err := d.Svc.AddMessage(r.Context(), ident(r), tid, b.Message) // simpan pesan
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusCreated, m)
}

// updateStatus: PATCH /api/tickets/{id}/status — ubah status tiket sesuai siklus hidup.
func (d *Deps) updateStatus(w http.ResponseWriter, r *http.Request) {
	tid, err := ticketID(r) // validasi ID
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct { // payload
		Status         string `json:"status"`          // status tujuan
		ResolutionNote string `json:"resolution_note"` // catatan penyelesaian (wajib untuk RESOLVED)
		Message        string `json:"message"`         // pesan (wajib untuk reopen)
	}
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	v.OneOf("status", b.Status, model.Statuses...) // status valid
	if len([]rune(b.ResolutionNote)) > 500 {
		v.Add("resolution_note", "maksimal 500 karakter")
	}
	if len([]rune(b.Message)) > 2000 {
		v.Add("message", "maksimal 2000 karakter")
	}
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	t, err := d.Svc.UpdateStatus(r.Context(), ident(r), tid, service.StatusInput{Status: b.Status, ResolutionNote: b.ResolutionNote, Message: b.Message}) // terapkan perubahan
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, t)
}
