// Package handlers: routing + guard RBAC + handler service-order (PRD 03 §2, §6).
package handlers

import (
	"net/http"
	"regexp"
	"slices"

	h "toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/validate"
	"toko-ai-microservices/service-order/internal/model"
	"toko-ai-microservices/service-order/internal/service"
)

type Deps struct{ Svc *service.Service }

var (
	orderIDRe   = regexp.MustCompile(`^ORD-\d{3,}$`)
	productIDRe = regexp.MustCompile(`^PRD-\d{3,}$`)
	userIDRe    = regexp.MustCompile(`^(USR|ADM|DEV)-\d{2,}$`)
)

func Register(mux *http.ServeMux, internalKey string, d *Deps) {
	secured := h.Chain(h.InternalAuthMiddleware(internalKey), h.IdentityMiddleware(), h.DenyGuest)
	customer := h.RequireRole(h.RoleCustomer)
	custOrAdmin := h.RequireRole(h.RoleCustomer, h.RoleAdminSistem)
	admin := h.RequireRole(h.RoleAdminSistem)

	mux.HandleFunc("GET /health", h.Health("service-order"))

	mux.Handle("POST /api/orders", secured(customer(http.HandlerFunc(d.create))))
	mux.Handle("GET /api/orders", secured(http.HandlerFunc(d.list)))
	mux.Handle("GET /api/orders/{id}", secured(http.HandlerFunc(d.get)))
	mux.Handle("POST /api/orders/{id}/pay", secured(customer(http.HandlerFunc(d.pay))))
	mux.Handle("POST /api/orders/{id}/cancel", secured(custOrAdmin(http.HandlerFunc(d.cancel))))
	mux.Handle("PATCH /api/orders/{id}/status", secured(admin(http.HandlerFunc(d.setStatus))))
}

// orderID memvalidasi {id}; format salah => 404.
func orderID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if !orderIDRe.MatchString(id) {
		return "", h.ErrNotFound("Pesanan " + id + " tidak ditemukan.")
	}
	return id, nil
}

func ident(r *http.Request) h.Identity { return h.IdentityFrom(r.Context()) }

type createBody struct {
	Items []struct {
		ProductID string `json:"product_id"`
		Quantity  int    `json:"quantity"`
	} `json:"items"`
	PaymentMethod   string `json:"payment_method"`
	ShippingAddress string `json:"shipping_address"`
	// Field harga (mis. "price") di body klien sengaja TIDAK dibaca — ReadJSON mengabaikan field tak dikenal.
}

func (d *Deps) create(w http.ResponseWriter, r *http.Request) {
	var b createBody
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	if n := len(b.Items); n < 1 || n > 20 {
		v.Add("items", "harus 1–20 item")
	}
	seen := map[string]bool{}
	in := service.CreateInput{PaymentMethod: b.PaymentMethod, ShippingAddress: b.ShippingAddress}
	for i, it := range b.Items {
		f := "items[" + itoa(i) + "]"
		if !productIDRe.MatchString(it.ProductID) {
			v.Add(f+".product_id", "format produk tidak valid")
		}
		if seen[it.ProductID] {
			v.Add(f+".product_id", "duplikat")
		}
		seen[it.ProductID] = true
		if it.Quantity < 1 || it.Quantity > 99 {
			v.Add(f+".quantity", "harus 1–99")
		}
		in.Items = append(in.Items, service.ItemInput{ProductID: it.ProductID, Quantity: it.Quantity})
	}
	v.OneOf("payment_method", b.PaymentMethod, "EWALLET", "BANK_TRANSFER", "CREDIT_CARD")
	v.Len("shipping_address", b.ShippingAddress, 10, 300)
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	o, err := d.Svc.Create(r.Context(), ident(r), in)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusCreated, o)
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

func (d *Deps) list(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	page, limit, err := h.Pagination(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	f := model.ListFilter{Status: r.URL.Query().Get("status"), Page: page, Limit: limit}
	if f.Status != "" && !slices.Contains(model.Statuses, f.Status) {
		h.Fail(w, r, h.ErrValidation("Parameter status tidak valid.", []h.FieldError{{Field: "status", Message: "harus salah satu dari: " + joinStatuses()}}))
		return
	}
	if id.Role == h.RoleCustomer {
		f.UserID = &id.UserID // CUSTOMER dipaksa ke data sendiri; ?user_id= diabaikan
	} else if u := r.URL.Query().Get("user_id"); u != "" {
		if !userIDRe.MatchString(u) {
			h.Fail(w, r, h.ErrValidation("Parameter user_id tidak valid.", []h.FieldError{{Field: "user_id", Message: "format tidak valid"}}))
			return
		}
		f.UserID = &u
	}
	orders, total, err := d.Svc.List(r.Context(), f)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteList(w, orders, h.PageMeta(total, page, limit))
}

func joinStatuses() string {
	s := ""
	for i, st := range model.Statuses {
		if i > 0 {
			s += ", "
		}
		s += st
	}
	return s
}

func (d *Deps) get(w http.ResponseWriter, r *http.Request) {
	oid, err := orderID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	o, err := d.Svc.Get(r.Context(), ident(r), oid)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, o)
}

func (d *Deps) pay(w http.ResponseWriter, r *http.Request) {
	oid, err := orderID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct {
		Simulate   string `json:"simulate"`
		FailReason string `json:"fail_reason"`
	}
	if err := h.ReadJSONOptional(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	if b.Simulate == "" {
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
	o, err := d.Svc.Pay(r.Context(), ident(r), oid, b.Simulate, b.FailReason)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, o)
}

func (d *Deps) cancel(w http.ResponseWriter, r *http.Request) {
	oid, err := orderID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct {
		Reason string `json:"reason"`
	}
	if err := h.ReadJSONOptional(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	if len([]rune(b.Reason)) > 200 {
		h.Fail(w, r, h.ErrValidation("Alasan maksimal 200 karakter.", []h.FieldError{{Field: "reason", Message: "maksimal 200 karakter"}}))
		return
	}
	o, err := d.Svc.Cancel(r.Context(), ident(r), oid, b.Reason)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, o)
}

func (d *Deps) setStatus(w http.ResponseWriter, r *http.Request) {
	oid, err := orderID(r)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	var b struct {
		Status         string `json:"status"`
		TrackingNumber string `json:"tracking_number"`
		Note           string `json:"note"`
	}
	if err := h.ReadJSON(w, r, &b); err != nil {
		h.Fail(w, r, err)
		return
	}
	v := validate.New()
	v.OneOf("status", b.Status, model.StatusProcessing, model.StatusShipped, model.StatusDelivered)
	if b.Status == model.StatusShipped {
		v.Len("tracking_number", b.TrackingNumber, 5, 40)
	}
	if len([]rune(b.Note)) > 200 {
		v.Add("note", "maksimal 200 karakter")
	}
	if e := v.Err(); e != nil {
		h.Fail(w, r, e)
		return
	}
	o, err := d.Svc.SetStatus(r.Context(), ident(r), oid, b.Status, b.TrackingNumber, b.Note)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.WriteData(w, http.StatusOK, o)
}
