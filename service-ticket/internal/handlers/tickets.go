package handlers

import (
	"net/http"

	h "toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/validate"
	"toko-ai-microservices/service-ticket/internal/model"
	"toko-ai-microservices/service-ticket/internal/service"
)

func ticketID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !ticketIDRe.MatchString(id) {
		return "", h.ErrNotFound("Tiket " + id + " tidak ditemukan.")
	}
	return id, nil
}

func (d *Deps) createTicket(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Subject     string `json:"subject"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Priority    string `json:"priority"`
		OrderID     string `json:"order_id"`
	}
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	v.Len("subject", b.Subject, 5, 120)
	v.Len("description", b.Description, 10, 2000)
	v.OneOf("category", b.Category, model.Categories...)
	if b.Priority != "" {
		v.OneOf("priority", b.Priority, model.Priorities...)
	}
	if b.OrderID != "" {
		v.Regex("order_id", b.OrderID, orderIDRe, "format order_id tidak valid")
	}
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	t, err := d.Svc.Create(r.Context(), ident(r), service.CreateInput{
		Subject: b.Subject, Description: b.Description, Category: b.Category, Priority: b.Priority, OrderID: b.OrderID,
	})
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusCreated, t)
}

func (d *Deps) listTickets(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	page, limit, err := h.Pagination(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	q := r.URL.Query()
	f := model.TicketFilter{Status: q.Get("status"), Category: q.Get("category"), Priority: q.Get("priority"), OrderID: q.Get("order_id"), Page: page, Limit: limit}
	v := validate.New()
	if f.Status != "" {
		v.OneOf("status", f.Status, model.Statuses...)
	}
	if f.Category != "" {
		v.OneOf("category", f.Category, model.Categories...)
	}
	if f.Priority != "" {
		v.OneOf("priority", f.Priority, model.Priorities...)
	}
	if f.OrderID != "" {
		v.Regex("order_id", f.OrderID, orderIDRe, "format order_id tidak valid")
	}
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	if id.Role == h.RoleCustomer {
		f.UserID = &id.UserID // dipaksa ke data sendiri; ?user_id= & ?assigned_to= diabaikan
	} else {
		if u := q.Get("user_id"); u != "" {
			if !userIDRe.MatchString(u) {
				h.Fail(w, r, h.ErrValidation("Parameter user_id tidak valid.", []h.FieldError{{Field: "user_id", Message: "format tidak valid"}}))
				return
			}
			f.UserID = &u
		}
		switch a := q.Get("assigned_to"); {
		case a == "me":
			f.AssignedTo = id.UserID
		case a != "":
			if !userIDRe.MatchString(a) {
				h.Fail(w, r, h.ErrValidation("Parameter assigned_to tidak valid.", []h.FieldError{{Field: "assigned_to", Message: "format tidak valid"}}))
				return
			}
			f.AssignedTo = a
		}
	}
	items, total, err := d.Svc.List(r.Context(), f)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteList(w, items, h.PageMeta(total, page, limit))
}

func (d *Deps) getTicket(w http.ResponseWriter, r *http.Request) {
	tid, err := ticketID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	t, err := d.Svc.Get(r.Context(), ident(r), tid)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, t)
}

func (d *Deps) addMessage(w http.ResponseWriter, r *http.Request) {
	tid, err := ticketID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct {
		Message string `json:"message"`
	}
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	v.Len("message", b.Message, 1, 2000)
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	m, err := d.Svc.AddMessage(r.Context(), ident(r), tid, b.Message)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusCreated, m)
}

func (d *Deps) updateStatus(w http.ResponseWriter, r *http.Request) {
	tid, err := ticketID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct {
		Status         string `json:"status"`
		ResolutionNote string `json:"resolution_note"`
		Message        string `json:"message"`
	}
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	v.OneOf("status", b.Status, model.Statuses...)
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
	t, err := d.Svc.UpdateStatus(r.Context(), ident(r), tid, service.StatusInput{Status: b.Status, ResolutionNote: b.ResolutionNote, Message: b.Message})
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, t)
}
