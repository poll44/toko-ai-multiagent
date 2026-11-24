package handlers_test // test black-box untuk handler service-order

import ( // package yang dipakai
	"context"           // context
	"encoding/json"     // encode/decode JSON
	"net/http"          // tipe HTTP
	"net/http/httptest" // server uji
	"path/filepath"     // path database sementara
	"strings"           // utilitas string
	"sync"              // mutex
	"testing"           // framework test
	"time"              // waktu

	"toko-ai-microservices/service-order/internal/catalogclient" // klien catalog
	"toko-ai-microservices/service-order/internal/handlers"      // paket yang diuji
	"toko-ai-microservices/service-order/internal/service"       // logika bisnis
	"toko-ai-microservices/service-order/internal/store"         // repository
)

// key: internal API key yang dipakai pada test.
const key = "order-test-key"

// fakeCatalog mensimulasikan kontrak /internal/stock/* service-catalog.
type fakeCatalog struct {
	mu       sync.Mutex     // melindungi state
	stock    map[string]int // stok per produk
	price    map[string]int // harga per produk
	restocks []string       // "ORD-xxx:PRD-yyy:qty"
	headers  []http.Header  // header tiap request yang diterima
	down     bool           // true = simulasi catalog mati
}

// newFakeCatalog menjalankan catalog palsu dengan stok & harga awal.
func newFakeCatalog(t *testing.T) (*httptest.Server, *fakeCatalog) {
	fc := &fakeCatalog{
		stock: map[string]int{"PRD-002": 25, "PRD-003": 80, "PRD-004": 200, "PRD-012": 8},                 // stok awal
		price: map[string]int{"PRD-002": 799000, "PRD-003": 299000, "PRD-004": 89000, "PRD-012": 1299000}, // harga
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fc.mu.Lock()
		defer fc.mu.Unlock()
		fc.headers = append(fc.headers, r.Header.Clone()) // rekam header
		if fc.down {                                      // simulasi koneksi terputus
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			c.Close()
			return
		}
		var body struct { // payload yang diterima
			ReferenceID string `json:"reference_id"`
			Items       []struct {
				ProductID string `json:"product_id"`
				Quantity  int    `json:"quantity"`
			} `json:"items"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/stock/decrement":
			// atomik: cek semua dulu
			for _, it := range body.Items {
				if fc.stock[it.ProductID] < it.Quantity { // stok kurang => 409
					w.WriteHeader(409)
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
						"code": "INSUFFICIENT_STOCK", "message": "Stok produk " + it.ProductID + " tidak mencukupi.",
						"details": map[string]any{"product_id": it.ProductID, "available": fc.stock[it.ProductID], "requested": it.Quantity}, "request_id": "x"}})
					return
				}
			}
			var items []map[string]any
			for _, it := range body.Items { // kurangi stok & bentuk hasil
				fc.stock[it.ProductID] -= it.Quantity
				items = append(items, map[string]any{"product_id": it.ProductID, "name": "Produk " + it.ProductID, "unit_price": fc.price[it.ProductID], "quantity": it.Quantity, "stock_after": fc.stock[it.ProductID]})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"reference_id": body.ReferenceID, "items": items}})
		case "/internal/stock/restock":
			for _, it := range body.Items { // kembalikan stok & catat
				fc.stock[it.ProductID] += it.Quantity
				fc.restocks = append(fc.restocks, body.ReferenceID+":"+it.ProductID)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"reference_id": body.ReferenceID, "restocked": true}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, fc
}

// env: lingkungan uji (server HTTP + catalog palsu + service).
type env struct {
	srv *httptest.Server // server service-order
	fc  *fakeCatalog     // catalog palsu
	svc *service.Service // service (untuk mengatur jam & menjalankan job)
	now time.Time        // waktu awal
}

// setup menyiapkan database sementara, seed, catalog palsu, dan server uji.
func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "order.db")) // database sementara
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := store.NewRepo(db)
	if err := repo.Seed(ctx); err != nil { // isi 8 pesanan contoh
		t.Fatal(err)
	}
	cs, fc := newFakeCatalog(t)
	svc := service.New(repo, catalogclient.New(cs.URL, key, 2*time.Second), 15*time.Minute) // service dengan TTL 15 menit
	mux := http.NewServeMux()
	handlers.Register(mux, key, &handlers.Deps{Svc: svc}) // daftarkan rute
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &env{srv: srv, fc: fc, svc: svc, now: time.Now()}
}

// resp: ringkasan respons HTTP.
type resp struct {
	Status int            // status code
	Body   map[string]any // body JSON
}

// users: pemetaan nama pelaku uji ke (id, role).
var users = map[string][2]string{
	"BUDI": {"USR-01", "CUSTOMER"}, "SITI": {"USR-02", "CUSTOMER"}, "ADMIN": {"ADM-01", "ADMIN_SISTEM"},
	"DEV": {"DEV-01", "ADMIN_DEV"}, "GUEST": {"GUEST", "GUEST"},
}

// do mengirim request sebagai pelaku `who` ("NOKEY" = tanpa internal key).
func (e *env) do(t *testing.T, who, method, path, body string) resp {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if who != "NOKEY" { // sertakan internal key
		req.Header.Set("x-internal-api-key", key)
	}
	if u, ok := users[who]; ok { // sertakan identitas
		req.Header.Set("x-user-id", u[0])
		req.Header.Set("x-user-role", u[1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return resp{res.StatusCode, m}
}

// code mengambil kode error dari respons ("" bila bukan error).
func code(r resp) string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

// data mengambil objek "data" dari respons.
func data(r resp) map[string]any { return r.Body["data"].(map[string]any) }

// ids mengambil daftar field "id" dari data respons daftar.
func ids(r resp) []string {
	var out []string
	for _, it := range r.Body["data"].([]any) {
		out = append(out, it.(map[string]any)["id"].(string))
	}
	return out
}

// newOrder: payload pesanan standar yang dipakai berulang.
const newOrder = `{"items":[{"product_id":"PRD-003","quantity":1}],"payment_method":"EWALLET","shipping_address":"Jl. Merdeka No. 10, Jakarta"}`

// TestAuthAndIsolation: autentikasi, isolasi data antar pelanggan, dan hak baca admin/dev.
func TestAuthAndIsolation(t *testing.T) {
	e := setup(t)
	if r := e.do(t, "NOKEY", "GET", "/api/orders", ""); r.Status != 401 || code(r) != "INVALID_INTERNAL_KEY" {
		t.Fatalf("tanpa key: %d %s", r.Status, code(r))
	}
	if r := e.do(t, "GUEST", "GET", "/api/orders", ""); r.Status != 403 {
		t.Fatalf("guest: %d", r.Status)
	}
	// seed: Budi hanya ORD-001..004
	got := ids(e.do(t, "BUDI", "GET", "/api/orders", ""))
	if strings.Join(got, ",") != "ORD-004,ORD-003,ORD-002,ORD-001" && len(got) != 4 {
		t.Fatalf("Budi: %v", got)
	}
	for _, id := range got {
		if id > "ORD-004" { // tidak boleh melihat pesanan orang lain
			t.Fatalf("Budi melihat order milik orang lain: %v", got)
		}
	}
	// ?user_id= diabaikan untuk customer
	if n := len(ids(e.do(t, "BUDI", "GET", "/api/orders?user_id=USR-02", ""))); n != 4 {
		t.Fatalf("customer harus dipaksa ke data sendiri: %d", n)
	}
	if r := e.do(t, "SITI", "GET", "/api/orders/ORD-003", ""); r.Status != 404 || code(r) != "NOT_FOUND" {
		t.Fatalf("Siti -> ORD-003: %d %s", r.Status, code(r))
	}
	if r := e.do(t, "BUDI", "GET", "/api/orders/ORD-003", ""); r.Status != 200 {
		t.Fatalf("Budi -> ORD-003: %d", r.Status)
	}
	// admin & dev membaca semua; admin filter user_id
	if n := len(ids(e.do(t, "DEV", "GET", "/api/orders?limit=100", ""))); n != 8 {
		t.Fatalf("dev harus melihat 8: %d", n)
	}
	if n := len(ids(e.do(t, "ADMIN", "GET", "/api/orders?user_id=USR-02", ""))); n != 4 {
		t.Fatalf("admin filter user: %d", n)
	}
	if r := e.do(t, "ADMIN", "GET", "/api/orders?status=BOGUS", ""); r.Status != 400 {
		t.Fatalf("status invalid: %d", r.Status)
	}
	// detail memuat history; field nullable eksplisit null
	d := data(e.do(t, "BUDI", "GET", "/api/orders/ORD-003", ""))
	if d["failure_reason"] != "Saldo e-wallet tidak mencukupi" || d["tracking_number"] != nil || len(d["history"].([]any)) != 2 {
		t.Fatalf("detail ORD-003: %v", d)
	}
	if _, ok := d["tracking_number"]; !ok {
		t.Fatal("field nullable harus selalu ada (null)")
	}
	if data(e.do(t, "BUDI", "GET", "/api/orders/ORD-002", ""))["tracking_number"] != "JNE-TRK-0002" {
		t.Fatal("resi ORD-002")
	}
}

// TestCreateOrderFlow: pembuatan pesanan memakai harga katalog (bukan klien) dan memotong stok.
func TestCreateOrderFlow(t *testing.T) {
	e := setup(t)
	// harga dari klien harus diabaikan; total dari katalog
	body := `{"items":[{"product_id":"PRD-003","quantity":2,"price":1},{"product_id":"PRD-004","quantity":1}],"price":1,"user_id":"USR-02","payment_method":"EWALLET","shipping_address":"Jl. Merdeka No. 10, Jakarta"}`
	r := e.do(t, "BUDI", "POST", "/api/orders", body)
	if r.Status != 201 {
		t.Fatalf("create: %d %v", r.Status, r.Body)
	}
	o := data(r)
	if o["id"] != "ORD-009" || o["user_id"] != "USR-01" || o["status"] != "PENDING_PAYMENT" || o["payment_status"] != "UNPAID" {
		t.Fatalf("order: %v", o)
	}
	if o["total_amount"].(float64) != 2*299000+89000 { // total dari katalog
		t.Fatalf("total harus dari katalog: %v", o["total_amount"])
	}
	exp, _ := time.Parse(time.RFC3339, o["payment_expires_at"].(string))
	if d := time.Until(exp); d < 14*time.Minute || d > 16*time.Minute { // batas bayar ±15 menit
		t.Fatalf("payment_expires_at ±15 menit: %v", d)
	}
	if e.fc.stock["PRD-003"] != 78 || e.fc.stock["PRD-004"] != 199 { // stok katalog berkurang
		t.Fatalf("stok katalog: %v", e.fc.stock)
	}
	// header identitas asli diteruskan ke catalog
	h := e.fc.headers[0]
	if h.Get("x-user-id") != "USR-01" || h.Get("x-service-name") != "service-order" || h.Get("x-internal-api-key") != key {
		t.Fatalf("header ke catalog: %v", h)
	}
	if r := e.do(t, "SITI", "GET", "/api/orders/ORD-009", ""); r.Status != 404 {
		t.Fatalf("order baru milik Budi tidak boleh terlihat Siti: %d", r.Status)
	}
}

// TestCreateOrderErrors: validasi input, hak akses, dan kegagalan catalog saat membuat pesanan.
func TestCreateOrderErrors(t *testing.T) {
	e := setup(t)
	before := len(ids(e.do(t, "ADMIN", "GET", "/api/orders?limit=100", ""))) // jumlah pesanan awal

	// stok kurang => 409 dari catalog diteruskan, tidak ada order baru, stok utuh
	r := e.do(t, "BUDI", "POST", "/api/orders", `{"items":[{"product_id":"PRD-012","quantity":9}],"payment_method":"EWALLET","shipping_address":"Jl. Merdeka No. 10, Jakarta"}`)
	if r.Status != 409 || code(r) != "INSUFFICIENT_STOCK" {
		t.Fatalf("stok kurang: %d %s", r.Status, code(r))
	}
	if d := r.Body["error"].(map[string]any)["details"].(map[string]any); d["product_id"] != "PRD-012" {
		t.Fatalf("details dipertahankan: %v", d)
	}
	// validasi
	for name, b := range map[string]string{
		"items kosong":    `{"items":[],"payment_method":"EWALLET","shipping_address":"Jl. Merdeka No. 10, Jakarta"}`,
		"produk duplikat": `{"items":[{"product_id":"PRD-003","quantity":1},{"product_id":"PRD-003","quantity":1}],"payment_method":"EWALLET","shipping_address":"Jl. Merdeka No. 10, Jakarta"}`,
		"qty 0":           `{"items":[{"product_id":"PRD-003","quantity":0}],"payment_method":"EWALLET","shipping_address":"Jl. Merdeka No. 10, Jakarta"}`,
		"metode salah":    `{"items":[{"product_id":"PRD-003","quantity":1}],"payment_method":"CASH","shipping_address":"Jl. Merdeka No. 10, Jakarta"}`,
		"alamat pendek":   `{"items":[{"product_id":"PRD-003","quantity":1}],"payment_method":"EWALLET","shipping_address":"x"}`,
		"id produk salah": `{"items":[{"product_id":"XYZ","quantity":1}],"payment_method":"EWALLET","shipping_address":"Jl. Merdeka No. 10, Jakarta"}`,
	} {
		if r := e.do(t, "BUDI", "POST", "/api/orders", b); r.Status != 400 || code(r) != "VALIDATION_ERROR" {
			t.Errorf("%s: %d %s", name, r.Status, code(r))
		}
	}
	// role: hanya CUSTOMER
	for _, who := range []string{"ADMIN", "DEV", "GUEST"} {
		if r := e.do(t, who, "POST", "/api/orders", newOrder); r.Status != 403 {
			t.Errorf("%s create: %d", who, r.Status)
		}
	}
	// catalog mati => 502, tidak ada order
	e.fc.mu.Lock()
	e.fc.down = true
	e.fc.mu.Unlock()
	if r := e.do(t, "BUDI", "POST", "/api/orders", newOrder); r.Status != 502 || code(r) != "UPSTREAM_ERROR" {
		t.Fatalf("catalog mati: %d %s", r.Status, code(r))
	}
	e.fc.mu.Lock()
	e.fc.down = false
	e.fc.mu.Unlock()
	if after := len(ids(e.do(t, "ADMIN", "GET", "/api/orders?limit=100", ""))); after != before {
		t.Fatalf("tidak boleh ada order baru: %d -> %d", before, after)
	}
}

// TestPaymentSimulations: simulasi pembayaran sukses/gagal/timeout, restock, dan aturan pemilik.
func TestPaymentSimulations(t *testing.T) {
	e := setup(t)
	mk := func() string { // pembuat pesanan baru, mengembalikan ID
		return data(e.do(t, "BUDI", "POST", "/api/orders", newOrder))["id"].(string)
	}
	// success
	id := mk()
	r := e.do(t, "BUDI", "POST", "/api/orders/"+id+"/pay", "") // body kosong => success
	if r.Status != 200 || data(r)["status"] != "PAID" || data(r)["payment_status"] != "PAID" || data(r)["paid_at"] == nil || data(r)["payment_expires_at"] != nil {
		t.Fatalf("pay success: %d %v", r.Status, r.Body)
	}
	// bayar dua kali => 409
	if r := e.do(t, "BUDI", "POST", "/api/orders/"+id+"/pay", `{"simulate":"success"}`); r.Status != 409 || code(r) != "INVALID_STATE_TRANSITION" {
		t.Fatalf("double pay: %d %s", r.Status, code(r))
	}
	// fail => PAYMENT_FAILED + restock
	stockBefore := e.fc.stock["PRD-003"]
	id = mk()
	r = e.do(t, "BUDI", "POST", "/api/orders/"+id+"/pay", `{"simulate":"fail"}`)
	if r.Status != 200 || data(r)["status"] != "PAYMENT_FAILED" || data(r)["failure_reason"] != "Pembayaran ditolak oleh penyedia pembayaran" {
		t.Fatalf("pay fail: %d %v", r.Status, r.Body)
	}
	if e.fc.stock["PRD-003"] != stockBefore { // dipotong 1 lalu dikembalikan 1 (+1 order success sebelumnya sudah dihitung di stockBefore)
		t.Fatalf("stok harus kembali: %d vs %d", e.fc.stock["PRD-003"], stockBefore)
	}
	// custom fail reason + timeout
	id = mk()
	r = e.do(t, "BUDI", "POST", "/api/orders/"+id+"/pay", `{"simulate":"fail","fail_reason":"Kartu ditolak bank"}`)
	if data(r)["failure_reason"] != "Kartu ditolak bank" {
		t.Fatalf("fail_reason: %v", data(r)["failure_reason"])
	}
	id = mk()
	r = e.do(t, "BUDI", "POST", "/api/orders/"+id+"/pay", `{"simulate":"timeout"}`)
	if data(r)["status"] != "PAYMENT_TIMEOUT" || data(r)["payment_status"] != "EXPIRED" {
		t.Fatalf("pay timeout: %v", r.Body)
	}
	// simulate invalid
	if r := e.do(t, "BUDI", "POST", "/api/orders/"+mk()+"/pay", `{"simulate":"magic"}`); r.Status != 400 {
		t.Fatalf("simulate invalid: %d", r.Status)
	}
	// bukan pemilik => 404 ; DEV/ADMIN tidak boleh bayar => 403
	id = mk()
	if r := e.do(t, "SITI", "POST", "/api/orders/"+id+"/pay", ""); r.Status != 404 {
		t.Fatalf("non-owner pay: %d", r.Status)
	}
	for _, who := range []string{"DEV", "ADMIN"} {
		if r := e.do(t, who, "POST", "/api/orders/"+id+"/pay", ""); r.Status != 403 {
			t.Fatalf("%s pay: %d", who, r.Status)
		}
	}
	// riwayat status tercatat
	h := data(e.do(t, "BUDI", "GET", "/api/orders/"+id, ""))["history"].([]any)
	if len(h) != 1 || h[0].(map[string]any)["to_status"] != "PENDING_PAYMENT" {
		t.Fatalf("history: %v", h)
	}
}

// TestPayAfterExpiry: membayar setelah batas waktu harus gagal dan pesanan menjadi PAYMENT_TIMEOUT + restock.
func TestPayAfterExpiry(t *testing.T) {
	e := setup(t)
	id := data(e.do(t, "BUDI", "POST", "/api/orders", newOrder))["id"].(string)
	stock := e.fc.stock["PRD-003"]
	e.svc.SetClock(func() time.Time { return time.Now().Add(16 * time.Minute) }) // majukan jam 16 menit
	r := e.do(t, "BUDI", "POST", "/api/orders/"+id+"/pay", `{"simulate":"success"}`)
	if r.Status != 409 || code(r) != "INVALID_STATE_TRANSITION" {
		t.Fatalf("pay setelah kedaluwarsa: %d %s", r.Status, code(r))
	}
	o := data(e.do(t, "BUDI", "GET", "/api/orders/"+id, ""))
	if o["status"] != "PAYMENT_TIMEOUT" || e.fc.stock["PRD-003"] != stock+1 {
		t.Fatalf("harus timeout + restock: %v stock=%d want=%d", o["status"], e.fc.stock["PRD-003"], stock+1)
	}
}

// TestCancel: aturan pembatalan untuk pelanggan dan admin (termasuk refund & restock).
func TestCancel(t *testing.T) {
	e := setup(t)
	id := data(e.do(t, "BUDI", "POST", "/api/orders", newOrder))["id"].(string)
	stock := e.fc.stock["PRD-003"]
	if r := e.do(t, "SITI", "POST", "/api/orders/"+id+"/cancel", ""); r.Status != 404 {
		t.Fatalf("non-owner cancel: %d", r.Status)
	}
	if r := e.do(t, "DEV", "POST", "/api/orders/"+id+"/cancel", ""); r.Status != 403 {
		t.Fatalf("dev cancel: %d", r.Status)
	}
	r := e.do(t, "BUDI", "POST", "/api/orders/"+id+"/cancel", `{"reason":"salah pilih"}`)
	if r.Status != 200 || data(r)["status"] != "CANCELLED" || e.fc.stock["PRD-003"] != stock+1 {
		t.Fatalf("cancel: %d %v stock=%d", r.Status, r.Body, e.fc.stock["PRD-003"])
	}
	// customer tidak boleh membatalkan order PAID (ORD-005 milik Siti, PROCESSING; ORD-001 milik Budi, DELIVERED)
	if r := e.do(t, "BUDI", "POST", "/api/orders/ORD-001/cancel", ""); r.Status != 409 {
		t.Fatalf("customer cancel DELIVERED: %d", r.Status)
	}
	if r := e.do(t, "SITI", "POST", "/api/orders/ORD-005/cancel", ""); r.Status != 409 {
		t.Fatalf("customer cancel PROCESSING: %d", r.Status)
	}
	// admin boleh membatalkan PROCESSING => REFUNDED + restock
	r = e.do(t, "ADMIN", "POST", "/api/orders/ORD-005/cancel", `{"reason":"permintaan pelanggan"}`)
	if r.Status != 200 || data(r)["status"] != "CANCELLED" || data(r)["payment_status"] != "REFUNDED" {
		t.Fatalf("admin cancel PROCESSING: %d %v", r.Status, r.Body)
	}
	// admin tidak boleh membatalkan order yang sudah final
	if r := e.do(t, "ADMIN", "POST", "/api/orders/ORD-001/cancel", ""); r.Status != 409 {
		t.Fatalf("admin cancel DELIVERED: %d", r.Status)
	}
}

// TestFulfilment: alur pemenuhan pesanan oleh admin (PROCESSING -> SHIPPED -> DELIVERED) dan batasannya.
func TestFulfilment(t *testing.T) {
	e := setup(t)
	id := data(e.do(t, "BUDI", "POST", "/api/orders", newOrder))["id"].(string)
	e.do(t, "BUDI", "POST", "/api/orders/"+id+"/pay", "")                                                    // bayar dulu
	patch := func(who, body string) resp { return e.do(t, who, "PATCH", "/api/orders/"+id+"/status", body) } // pembantu PATCH status

	if r := patch("BUDI", `{"status":"PROCESSING"}`); r.Status != 403 {
		t.Fatalf("customer patch: %d", r.Status)
	}
	if r := patch("DEV", `{"status":"PROCESSING"}`); r.Status != 403 {
		t.Fatalf("dev patch: %d", r.Status)
	}
	if r := patch("ADMIN", `{"status":"SHIPPED","tracking_number":"JNE-0001"}`); r.Status != 409 {
		t.Fatalf("PAID -> SHIPPED harus ditolak: %d", r.Status)
	}
	if r := patch("ADMIN", `{"status":"PROCESSING"}`); r.Status != 200 {
		t.Fatalf("PROCESSING: %d", r.Status)
	}
	if r := patch("ADMIN", `{"status":"SHIPPED"}`); r.Status != 400 { // resi wajib
		t.Fatalf("SHIPPED tanpa resi: %d", r.Status)
	}
	r := patch("ADMIN", `{"status":"SHIPPED","tracking_number":"JNE-TRK-0099","note":"dikirim"}`)
	if r.Status != 200 || data(r)["tracking_number"] != "JNE-TRK-0099" {
		t.Fatalf("SHIPPED: %d %v", r.Status, r.Body)
	}
	if r := patch("ADMIN", `{"status":"DELIVERED"}`); r.Status != 200 || data(r)["status"] != "DELIVERED" {
		t.Fatalf("DELIVERED: %d", r.Status)
	}
	if r := patch("ADMIN", `{"status":"PROCESSING"}`); r.Status != 409 {
		t.Fatalf("mundur harus ditolak: %d", r.Status)
	}
	if r := patch("ADMIN", `{"status":"PAID"}`); r.Status != 400 {
		t.Fatalf("status non-fulfilment: %d", r.Status)
	}
	// history lengkap
	h := data(e.do(t, "BUDI", "GET", "/api/orders/"+id, ""))["history"].([]any)
	if len(h) != 5 {
		t.Fatalf("history harus 5 entri: %d", len(h))
	}
}

// TestExpiryJob: job kedaluwarsa menandai pesanan lewat batas, mengembalikan stok, dan idempoten.
func TestExpiryJob(t *testing.T) {
	e := setup(t)
	// ORD-006 (Siti, PRD-002) kedaluwarsa 10 menit setelah seed
	if n, err := e.svc.ExpireDue(context.Background()); err != nil || n != 0 { // belum lewat batas
		t.Fatalf("belum kedaluwarsa: n=%d err=%v", n, err)
	}
	e.svc.SetClock(func() time.Time { return time.Now().Add(11 * time.Minute) }) // majukan jam 11 menit
	stock := e.fc.stock["PRD-002"]
	n, err := e.svc.ExpireDue(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("harus 1 order diproses: n=%d err=%v", n, err)
	}
	o := data(e.do(t, "SITI", "GET", "/api/orders/ORD-006", ""))
	if o["status"] != "PAYMENT_TIMEOUT" || o["payment_status"] != "EXPIRED" || e.fc.stock["PRD-002"] != stock+1 {
		t.Fatalf("ORD-006: %v stok=%d want=%d", o, e.fc.stock["PRD-002"], stock+1)
	}
	h := o["history"].([]any)
	if last := h[len(h)-1].(map[string]any); last["actor_id"] != "SYSTEM" || last["to_status"] != "PAYMENT_TIMEOUT" { // dicatat oleh SYSTEM
		t.Fatalf("history job: %v", last)
	}
	// idempoten
	if n, _ := e.svc.ExpireDue(context.Background()); n != 0 {
		t.Fatalf("job kedua harus no-op: %d", n)
	}
}
