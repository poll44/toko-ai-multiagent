package handlers_test // test black-box untuk handler service-ticket

import ( // package yang dipakai
	"context"           // context
	"encoding/json"     // encode/decode JSON
	"hash/fnv"          // hash kata untuk fake embedder
	"net/http"          // tipe HTTP
	"net/http/httptest" // server uji
	"path/filepath"     // path database sementara
	"regexp"            // pemecah kata
	"strings"           // utilitas string
	"sync/atomic"       // penghitung & flag atomik
	"testing"           // framework test
	"time"              // timeout

	"toko-ai-microservices/internal/shared/embedding"           // tipe Task & ErrUnavailable
	"toko-ai-microservices/internal/shared/vector"              // normalisasi vektor
	"toko-ai-microservices/service-ticket/internal/handlers"    // paket yang diuji
	"toko-ai-microservices/service-ticket/internal/knowledge"   // indexer & seed KB
	"toko-ai-microservices/service-ticket/internal/orderclient" // klien order
	"toko-ai-microservices/service-ticket/internal/service"     // logika bisnis
	"toko-ai-microservices/service-ticket/internal/store"       // repository
)

// key: internal API key yang dipakai pada test.
const key = "ticket-test-key"

// fakeEmbedder: bag-of-words hashing 1024 dimensi — deterministik, tanpa jaringan (leksikal, bukan semantik sungguhan).
type fakeEmbedder struct{ enabled bool }

var wordRe = regexp.MustCompile(`[a-z0-9]+`)                                                                                                                     // pemecah kata
var stop = map[string]bool{"yang": true, "dan": true, "untuk": true, "dengan": true, "apa": true, "berapa": true, "bagaimana": true, "atau": true, "dari": true} // kata yang diabaikan

// Enabled: apakah embedder aktif.
func (f fakeEmbedder) Enabled() bool { return f.enabled }

// Model: nama model palsu.
func (f fakeEmbedder) Model() string { return "fake-bow" }

// Dim: dimensi vektor palsu.
func (f fakeEmbedder) Dim() int { return 1024 }

// Embed membuat vektor dengan menghitung hash tiap kata ke 1024 slot, lalu menormalkannya.
func (f fakeEmbedder) Embed(_ context.Context, text string, _ embedding.Task) ([]float32, error) {
	if !f.enabled { // embedding mati
		return nil, embedding.ErrUnavailable
	}
	v := make([]float32, 1024)
	for _, w := range wordRe.FindAllString(strings.ToLower(text), -1) { // tiap kata
		if stop[w] { // lewati stopword
			continue
		}
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%1024]++ // tambah hitungan di slot hasil hash
	}
	return vector.Normalize(v), nil
}

// EmbedBatch meng-embed banyak teks satu per satu.
func (f fakeEmbedder) EmbedBatch(ctx context.Context, texts []string, t embedding.Task) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, s := range texts {
		v, err := f.Embed(ctx, s, t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// fakeOrder: service-order palsu; kepemilikan order ditentukan dari x-user-id.
type fakeOrder struct {
	down  atomic.Bool  // true = simulasi service-order mati
	calls atomic.Int32 // jumlah panggilan yang diterima
}

// orderOwners: pemilik tiap pesanan pada data palsu.
var orderOwners = map[string]string{"ORD-001": "USR-01", "ORD-003": "USR-01", "ORD-005": "USR-02", "ORD-008": "USR-02"}

// newFakeOrder menjalankan service-order palsu.
func newFakeOrder(t *testing.T) (*httptest.Server, *fakeOrder) {
	fo := &fakeOrder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fo.calls.Add(1)     // hitung panggilan
		if fo.down.Load() { // simulasi koneksi terputus
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			c.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/api/orders/") // ID pesanan dari path
		if r.Header.Get("x-internal-api-key") != key || r.Header.Get("x-service-name") != "service-ticket" ||
			orderOwners[id] != r.Header.Get("x-user-id") { // kunci/pemanggil salah atau bukan pemilik => 404
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"Pesanan tidak ditemukan.","details":null,"request_id":"x"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": id, "user_id": orderOwners[id], "status": "PAYMENT_FAILED"}})
	}))
	t.Cleanup(srv.Close)
	return srv, fo
}

// env: lingkungan uji (server HTTP + order palsu + indexer).
type env struct {
	srv *httptest.Server   // server service-ticket
	fo  *fakeOrder         // order palsu
	ix  *knowledge.Indexer // indexer KB
}

// setup menyiapkan database sementara, seed tiket & KB, indexer, order palsu, dan server uji.
func setup(t *testing.T, embEnabled bool) *env {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "ticket.db")) // database sementara
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := store.NewRepo(db)
	if err := repo.SeedTickets(ctx); err != nil { // isi 5 tiket contoh
		t.Fatal(err)
	}
	if err := knowledge.SeedDocs(ctx, repo); err != nil { // isi 8 dokumen kebijakan
		t.Fatal(err)
	}
	ix := knowledge.NewIndexer(repo, fakeEmbedder{enabled: embEnabled})
	if embEnabled { // indeks semua chunk
		if _, err := ix.IndexPending(ctx, false, 0); err != nil {
			t.Fatal(err)
		}
	}
	os, fo := newFakeOrder(t)
	svc := service.New(repo, orderclient.New(os.URL, key, 2*time.Second)) // service dengan klien order palsu
	mux := http.NewServeMux()
	handlers.Register(mux, key, &handlers.Deps{Svc: svc, Repo: repo, Index: ix, MinScore: 0.05}) // daftarkan rute
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &env{srv: srv, fo: fo, ix: ix}
}

// resp: ringkasan respons HTTP.
type resp struct {
	Status int            // status code
	Body   map[string]any // body JSON
	Header http.Header    // header respons
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
	return resp{res.StatusCode, m, res.Header}
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

// has: true bila list memuat s.
func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ───────────────────────── tiket ─────────────────────────

// TestAuthAndIsolation: autentikasi, isolasi tiket antar pelanggan, filter, dan detail tiket.
func TestAuthAndIsolation(t *testing.T) {
	e := setup(t, false)
	if r := e.do(t, "NOKEY", "GET", "/api/tickets", ""); r.Status != 401 || code(r) != "INVALID_INTERNAL_KEY" {
		t.Fatalf("tanpa key: %d %s", r.Status, code(r))
	}
	if r := e.do(t, "GUEST", "GET", "/api/tickets", ""); r.Status != 403 {
		t.Fatalf("guest tiket: %d", r.Status)
	}
	if got := ids(e.do(t, "BUDI", "GET", "/api/tickets", "")); len(got) != 2 || !has(got, "TKT-001") || !has(got, "TKT-002") {
		t.Fatalf("Budi: %v", got)
	}
	if got := ids(e.do(t, "BUDI", "GET", "/api/tickets?user_id=USR-02", "")); len(got) != 2 {
		t.Fatalf("customer dipaksa ke data sendiri: %v", got)
	}
	if r := e.do(t, "BUDI", "GET", "/api/tickets/TKT-003", ""); r.Status != 404 || code(r) != "NOT_FOUND" {
		t.Fatalf("Budi -> TKT-003: %d %s", r.Status, code(r))
	}
	for _, who := range []string{"ADMIN", "DEV"} { // admin & dev melihat semua tiket
		if n := len(ids(e.do(t, who, "GET", "/api/tickets?limit=100", ""))); n != 5 {
			t.Fatalf("%s harus melihat 5 tiket: %d", who, n)
		}
	}
	if got := ids(e.do(t, "ADMIN", "GET", "/api/tickets?assigned_to=me", "")); len(got) != 3 {
		t.Fatalf("assigned_to=me: %v", got)
	}
	if got := ids(e.do(t, "ADMIN", "GET", "/api/tickets?category=TECHNICAL", "")); len(got) != 1 || got[0] != "TKT-004" {
		t.Fatalf("filter kategori: %v", got)
	}
	if r := e.do(t, "ADMIN", "GET", "/api/tickets?status=BOGUS", ""); r.Status != 400 {
		t.Fatalf("status invalid: %d", r.Status)
	}
	// detail: pesan + history; field nullable eksplisit
	d := data(e.do(t, "BUDI", "GET", "/api/tickets/TKT-001", ""))
	if len(d["messages"].([]any)) != 1 || len(d["history"].([]any)) != 1 || d["assigned_to"] != nil {
		t.Fatalf("detail TKT-001: %v", d)
	}
	if got := data(e.do(t, "BUDI", "GET", "/api/tickets/TKT-002", "")); got["status"] != "RESOLVED" || got["resolution_note"] == nil || len(got["history"].([]any)) != 3 {
		t.Fatalf("TKT-002: %v", got)
	}
}

// TestCreateTicket: pembuatan tiket (validasi order ke service-order, duplikat, prioritas, validasi, role).
func TestCreateTicket(t *testing.T) {
	e := setup(t, false)
	mk := func(who, body string) resp { return e.do(t, who, "POST", "/api/tickets", body) } // pembantu membuat tiket
	const base = `"subject":"Barang bermasalah","description":"Deskripsi masalah yang cukup panjang.","category":"PRODUCT"`

	// sukses: order milik sendiri tanpa tiket aktif (TKT-002 sudah RESOLVED)
	r := mk("BUDI", `{`+base+`,"order_id":"ORD-001","user_id":"USR-02","status":"CLOSED"}`)
	if r.Status != 201 {
		t.Fatalf("create: %d %v", r.Status, r.Body)
	}
	tk := data(r)
	if tk["id"] != "TKT-006" || tk["user_id"] != "USR-01" || tk["status"] != "OPEN" || tk["priority"] != "MEDIUM" {
		t.Fatalf("tiket: %v", tk)
	}
	if len(tk["messages"].([]any)) != 1 || tk["messages"].([]any)[0].(map[string]any)["message"] != "Deskripsi masalah yang cukup panjang." {
		t.Fatalf("pesan awal = description: %v", tk["messages"])
	}
	// order milik user lain => 404 dengan details.field
	r = mk("BUDI", `{`+base+`,"order_id":"ORD-005"}`)
	if r.Status != 404 || code(r) != "NOT_FOUND" {
		t.Fatalf("order orang lain: %d %s", r.Status, code(r))
	}
	if d := r.Body["error"].(map[string]any)["details"].(map[string]any); d["field"] != "order_id" {
		t.Fatalf("details: %v", d)
	}
	// duplikat aktif: ORD-003 sudah punya TKT-001 OPEN
	r = mk("BUDI", `{`+base+`,"order_id":"ORD-003"}`)
	if r.Status != 409 || code(r) != "CONFLICT" {
		t.Fatalf("duplikat: %d %s", r.Status, code(r))
	}
	if d := r.Body["error"].(map[string]any)["details"].(map[string]any); d["existing_ticket_id"] != "TKT-001" {
		t.Fatalf("existing_ticket_id: %v", d)
	}
	// TECHNICAL + LOW => MEDIUM; tanpa order tidak memanggil service-order
	calls := e.fo.calls.Load()
	r = mk("SITI", `{"subject":"Halaman error","description":"Halaman produk error saat dibuka.","category":"TECHNICAL","priority":"LOW"}`)
	if r.Status != 201 || data(r)["priority"] != "MEDIUM" || data(r)["order_id"] != nil || e.fo.calls.Load() != calls {
		t.Fatalf("technical: %d %v calls=%d->%d", r.Status, r.Body, calls, e.fo.calls.Load())
	}
	// order service mati => 502 dan tiket TIDAK dibuat
	before := len(ids(e.do(t, "ADMIN", "GET", "/api/tickets?limit=100", "")))
	e.fo.down.Store(true)
	if r := mk("SITI", `{`+base+`,"order_id":"ORD-008"}`); r.Status != 502 || code(r) != "UPSTREAM_ERROR" {
		t.Fatalf("order down: %d %s", r.Status, code(r))
	}
	e.fo.down.Store(false)
	if after := len(ids(e.do(t, "ADMIN", "GET", "/api/tickets?limit=100", ""))); after != before {
		t.Fatalf("tiket tidak boleh terbuat: %d -> %d", before, after)
	}
	// validasi & role
	for name, b := range map[string]string{
		"subject pendek": `{"subject":"x","description":"Deskripsi masalah yang cukup panjang.","category":"PRODUCT"}`,
		"desc pendek":    `{"subject":"Subjek valid","description":"pendek","category":"PRODUCT"}`,
		"kategori salah": `{"subject":"Subjek valid","description":"Deskripsi masalah yang cukup panjang.","category":"X"}`,
		"order id salah": `{` + base + `,"order_id":"123"}`,
		"prioritas":      `{` + base + `,"priority":"URGENT"}`,
	} {
		if r := mk("BUDI", b); r.Status != 400 || code(r) != "VALIDATION_ERROR" {
			t.Errorf("%s: %d %s", name, r.Status, code(r))
		}
	}
	for _, who := range []string{"ADMIN", "DEV", "GUEST"} { // hanya CUSTOMER yang boleh membuat tiket
		if r := mk(who, `{`+base+`}`); r.Status != 403 {
			t.Errorf("%s create: %d", who, r.Status)
		}
	}
}

// TestLifecycleAndRBAC: siklus hidup status tiket dan pembatasan hak akses per role.
func TestLifecycleAndRBAC(t *testing.T) {
	e := setup(t, false)
	patch := func(who, tid, body string) resp { return e.do(t, who, "PATCH", "/api/tickets/"+tid+"/status", body) } // pembantu PATCH status

	// TKT-004 (TECHNICAL, milik Siti)
	if r := patch("SITI", "TKT-004", `{"status":"IN_PROGRESS"}`); r.Status != 403 {
		t.Fatalf("customer tidak boleh IN_PROGRESS: %d", r.Status)
	}
	if r := patch("BUDI", "TKT-004", `{"status":"CLOSED"}`); r.Status != 404 {
		t.Fatalf("non-owner: %d", r.Status)
	}
	r := patch("DEV", "TKT-004", `{"status":"IN_PROGRESS"}`)
	if r.Status != 200 || data(r)["status"] != "IN_PROGRESS" || data(r)["assigned_to"] != "DEV-01" {
		t.Fatalf("dev start: %d %v", r.Status, r.Body)
	}
	if r := patch("DEV", "TKT-004", `{"status":"RESOLVED"}`); r.Status != 400 {
		t.Fatalf("RESOLVED tanpa note: %d", r.Status)
	}
	r = patch("DEV", "TKT-004", `{"status":"RESOLVED","resolution_note":"Bug checkout e-wallet sudah diperbaiki."}`)
	if r.Status != 200 || data(r)["resolved_at"] == nil || data(r)["resolution_note"] == nil {
		t.Fatalf("resolve: %d %v", r.Status, r.Body)
	}
	// reopen wajib pesan
	if r := patch("SITI", "TKT-004", `{"status":"OPEN"}`); r.Status != 400 {
		t.Fatalf("reopen tanpa message: %d", r.Status)
	}
	r = patch("SITI", "TKT-004", `{"status":"OPEN","message":"Masih error saat memilih e-wallet."}`)
	if r.Status != 200 || data(r)["status"] != "OPEN" || data(r)["resolved_at"] != nil {
		t.Fatalf("reopen: %d %v", r.Status, r.Body)
	}
	msgs := data(r)["messages"].([]any)
	if last := msgs[len(msgs)-1].(map[string]any); last["message"] != "Masih error saat memilih e-wallet." || last["author_id"] != "USR-02" || last["author_role"] != "CUSTOMER" {
		t.Fatalf("pesan reopen: %v", last)
	}
	// graf: OPEN -> RESOLVED langsung ditolak
	if r := patch("DEV", "TKT-004", `{"status":"RESOLVED","resolution_note":"catatan yang cukup panjang"}`); r.Status != 409 || code(r) != "INVALID_STATE_TRANSITION" {
		t.Fatalf("OPEN->RESOLVED: %d %s", r.Status, code(r))
	}
	// customer menutup dari OPEN
	r = patch("SITI", "TKT-004", `{"status":"CLOSED"}`)
	if r.Status != 200 || data(r)["closed_at"] == nil {
		t.Fatalf("close: %d %v", r.Status, r.Body)
	}
	// CLOSED final
	for _, who := range []string{"SITI", "ADMIN", "DEV"} {
		for _, st := range []string{"OPEN", "IN_PROGRESS", "RESOLVED", "CLOSED"} {
			if r := patch(who, "TKT-004", `{"status":"`+st+`","message":"x","resolution_note":"catatan yang cukup panjang"}`); r.Status != 409 {
				t.Errorf("%s CLOSED->%s: %d", who, st, r.Status)
			}
		}
	}
	if r := e.do(t, "SITI", "POST", "/api/tickets/TKT-004/messages", `{"message":"halo"}`); r.Status != 409 {
		t.Fatalf("pesan ke CLOSED: %d", r.Status)
	}
	// history lengkap: OPEN, IN_PROGRESS, RESOLVED, OPEN, CLOSED
	h := data(e.do(t, "ADMIN", "GET", "/api/tickets/TKT-004", ""))["history"].([]any)
	if len(h) != 5 {
		t.Fatalf("history: %d", len(h))
	}

	// ADMIN_DEV tidak boleh menyentuh kategori non-TECHNICAL
	if r := patch("DEV", "TKT-001", `{"status":"IN_PROGRESS"}`); r.Status != 403 {
		t.Fatalf("dev PAYMENT status: %d", r.Status)
	}
	if r := e.do(t, "DEV", "POST", "/api/tickets/TKT-001/messages", `{"message":"coba"}`); r.Status != 403 {
		t.Fatalf("dev PAYMENT message: %d", r.Status)
	}
	// ADMIN_SISTEM menangani PAYMENT: start -> release -> start -> resolve; resolve admin ke TKT-001
	if r := patch("ADMIN", "TKT-001", `{"status":"IN_PROGRESS"}`); r.Status != 200 || data(r)["assigned_to"] != "ADM-01" {
		t.Fatalf("admin start: %d", r.Status)
	}
	if r := patch("ADMIN", "TKT-001", `{"status":"OPEN"}`); r.Status != 200 || data(r)["assigned_to"] != nil {
		t.Fatalf("release: %d %v", r.Status, r.Body)
	}
	// RESOLVED -> CLOSED oleh customer pemilik (TKT-002 milik Budi)
	if r := patch("BUDI", "TKT-002", `{"status":"CLOSED"}`); r.Status != 200 {
		t.Fatalf("close resolved: %d", r.Status)
	}
	if r := patch("ADMIN", "TKT-001", `{"status":"BOGUS"}`); r.Status != 400 {
		t.Fatalf("status bogus: %d", r.Status)
	}
}

// TestMessages: penambahan pesan oleh pelanggan/admin/dev dan batasannya.
func TestMessages(t *testing.T) {
	e := setup(t, false)
	post := func(who, tid, body string) resp { return e.do(t, who, "POST", "/api/tickets/"+tid+"/messages", body) } // pembantu kirim pesan
	r := post("BUDI", "TKT-001", `{"message":"Saya sudah isi saldo lagi."}`)
	if r.Status != 201 || data(r)["author_id"] != "USR-01" || data(r)["author_role"] != "CUSTOMER" {
		t.Fatalf("customer msg: %d %v", r.Status, r.Body)
	}
	if r := post("ADMIN", "TKT-001", `{"message":"Kami cek dulu ya."}`); r.Status != 201 || data(r)["author_role"] != "ADMIN_SISTEM" {
		t.Fatalf("admin msg: %d", r.Status)
	}
	if r := post("SITI", "TKT-001", `{"message":"nyelonong"}`); r.Status != 404 {
		t.Fatalf("non-owner msg: %d", r.Status)
	}
	if r := post("BUDI", "TKT-001", `{"message":""}`); r.Status != 400 {
		t.Fatalf("pesan kosong: %d", r.Status)
	}
	if r := post("DEV", "TKT-004", `{"message":"Sedang diselidiki."}`); r.Status != 201 {
		t.Fatalf("dev TECHNICAL msg: %d", r.Status)
	}
	if r := post("SITI", "TKT-005", `{"message":"halo"}`); r.Status != 409 {
		t.Fatalf("CLOSED msg: %d", r.Status)
	}
	d := data(e.do(t, "BUDI", "GET", "/api/tickets/TKT-001", ""))
	if len(d["messages"].([]any)) != 3 { // 1 awal + 2 baru
		t.Fatalf("jumlah pesan: %d", len(d["messages"].([]any)))
	}
}

// ───────────────────────── knowledge base ─────────────────────────

// TestKnowledgeSemanticSearch: pencarian KB semantik (peringkat, maks 2 chunk/dokumen, filter, validasi).
func TestKnowledgeSemanticSearch(t *testing.T) {
	e := setup(t, true)
	search := func(who, q string) resp { // pembantu pencarian
		return e.do(t, who, "GET", "/api/knowledge/search?q="+strings.ReplaceAll(q, " ", "%20"), "")
	}
	// GUEST boleh
	r := search("GUEST", "berapa lama batas waktu pembayaran")
	if r.Status != 200 {
		t.Fatalf("status %d: %v", r.Status, r.Body)
	}
	got := r.Body["data"].([]any)
	if len(got) == 0 || got[0].(map[string]any)["document_id"] != "KB-005" { // KB-005 peringkat 1
		t.Fatalf("KB-005 harus peringkat 1: %v", got)
	}
	first := got[0].(map[string]any)
	if first["score"] == nil || first["content"] == "" || first["title"] != "Pembayaran Gagal dan Kedaluwarsa" || first["category"] != "PAYMENT" {
		t.Fatalf("field hasil: %v", first)
	}
	if m := r.Body["meta"].(map[string]any); m["mode"] != "semantic" || m["limit"].(float64) != 3 {
		t.Fatalf("meta: %v", m)
	}
	// retur
	r = search("BUDI", "syarat pengembalian barang rusak")
	if got := r.Body["data"].([]any); len(got) == 0 || got[0].(map[string]any)["document_id"] != "KB-001" {
		t.Fatalf("KB-001: %v", got)
	}
	// maks 2 chunk per dokumen & limit
	r = e.do(t, "GUEST", "GET", "/api/knowledge/search?q=pesanan%20pembayaran%20refund%20retur%20garansi&limit=5", "")
	perDoc := map[string]int{} // jumlah chunk per dokumen
	for _, it := range r.Body["data"].([]any) {
		perDoc[it.(map[string]any)["document_id"].(string)]++
	}
	for d, n := range perDoc {
		if n > 2 {
			t.Fatalf("dokumen %s memiliki %d chunk (maks 2)", d, n)
		}
	}
	// filter kategori
	r = e.do(t, "GUEST", "GET", "/api/knowledge/search?q=pesanan%20pembayaran%20pembatalan&category=ORDER", "")
	for _, it := range r.Body["data"].([]any) {
		if it.(map[string]any)["category"] != "ORDER" {
			t.Fatalf("filter kategori: %v", it)
		}
	}
	// tidak relevan => kosong, 200
	r = e.do(t, "GUEST", "GET", "/api/knowledge/search?q=resep%20rendang%20padang%20enak&min_score=0.2", "")
	if r.Status != 200 || len(r.Body["data"].([]any)) != 0 {
		t.Fatalf("query tak relevan harus kosong: %d %v", r.Status, r.Body["data"])
	}
	// validasi
	for _, p := range []string{"q=ab", "q=", "q=abc&limit=9", "q=abc&category=BOGUS", "q=abc&min_score=2"} {
		if r := e.do(t, "GUEST", "GET", "/api/knowledge/search?"+p, ""); r.Status != 400 {
			t.Errorf("%s: %d", p, r.Status)
		}
	}
	if r := e.do(t, "NOKEY", "GET", "/api/knowledge/search?q=retur", ""); r.Status != 401 {
		t.Fatalf("tanpa key: %d", r.Status)
	}
}

// TestKnowledgeKeywordFallback: tanpa embedding, pencarian KB jatuh ke keyword_fallback dengan skor null.
func TestKnowledgeKeywordFallback(t *testing.T) {
	e := setup(t, false) // embedding mati
	r := e.do(t, "GUEST", "GET", "/api/knowledge/search?q=garansi%20bulan", "")
	if r.Status != 200 || r.Body["meta"].(map[string]any)["mode"] != "keyword_fallback" || r.Header.Get("x-search-mode") != "keyword_fallback" {
		t.Fatalf("fallback: %d %v", r.Status, r.Body["meta"])
	}
	got := r.Body["data"].([]any)
	if len(got) == 0 || got[0].(map[string]any)["document_id"] != "KB-002" || got[0].(map[string]any)["score"] != nil {
		t.Fatalf("hasil fallback: %v", got)
	}
}

// TestKnowledgeDocumentsAdmin: CRUD dokumen KB (hak akses, indexing otomatis, soft delete, reindex).
func TestKnowledgeDocumentsAdmin(t *testing.T) {
	e := setup(t, true)
	body := `{"title":"Program Cicilan Nol Persen","category":"PAYMENT","content":"Toko menyediakan program cicilan nol persen untuk pembelian kartu kredit tertentu selama 3 bulan."}`
	for _, who := range []string{"BUDI", "DEV", "GUEST"} { // hanya ADMIN_SISTEM yang boleh membuat
		if r := e.do(t, who, "POST", "/api/knowledge/documents", body); r.Status != 403 {
			t.Errorf("%s create: %d", who, r.Status)
		}
	}
	// publik membaca
	r := e.do(t, "GUEST", "GET", "/api/knowledge/documents", "")
	docs := r.Body["data"].([]any)
	if r.Status != 200 || len(docs) != 8 {
		t.Fatalf("daftar: %d %d", r.Status, len(docs))
	}
	if _, has := docs[0].(map[string]any)["content"]; has { // daftar tanpa content
		t.Fatal("daftar tidak boleh memuat content")
	}
	if docs[0].(map[string]any)["chunk_count"].(float64) < 1 {
		t.Fatal("chunk_count")
	}
	d := data(e.do(t, "GUEST", "GET", "/api/knowledge/documents/KB-005", ""))
	if d["title"] != "Pembayaran Gagal dan Kedaluwarsa" || !strings.Contains(d["content"].(string), "15 menit") {
		t.Fatalf("dokumen: %v", d)
	}
	if r := e.do(t, "GUEST", "GET", "/api/knowledge/documents/KB-099", ""); r.Status != 404 {
		t.Fatalf("tidak ada: %d", r.Status)
	}

	// admin membuat => langsung dapat dicari (embedding sinkron)
	r = e.do(t, "ADMIN", "POST", "/api/knowledge/documents", body)
	if r.Status != 201 || data(r)["id"] != "KB-009" || data(r)["chunk_count"].(float64) != 1 {
		t.Fatalf("create: %d %v", r.Status, r.Body)
	}
	sr := e.do(t, "GUEST", "GET", "/api/knowledge/search?q=cicilan%20nol%20persen&min_score=0.3", "")
	if got := sr.Body["data"].([]any); len(got) == 0 || got[0].(map[string]any)["document_id"] != "KB-009" {
		t.Fatalf("dokumen baru harus ditemukan: %v", got)
	}
	// validasi
	if r := e.do(t, "ADMIN", "POST", "/api/knowledge/documents", `{"title":"x","category":"BAD","content":"pendek"}`); r.Status != 400 {
		t.Fatalf("validasi: %d", r.Status)
	}
	// update => re-chunk, hasil lama tak ditemukan
	r = e.do(t, "ADMIN", "PUT", "/api/knowledge/documents/KB-009", `{"title":"Program Cicilan Nol Persen","category":"PAYMENT","content":"Program cicilan dihentikan sementara; hubungi tim melalui tiket untuk informasi pengganti program promosi lain."}`)
	if r.Status != 200 {
		t.Fatalf("update: %d %v", r.Status, r.Body)
	}
	// soft delete => hilang untuk publik & search; admin masih melihat
	if r := e.do(t, "ADMIN", "DELETE", "/api/knowledge/documents/KB-009", ""); r.Status != 204 {
		t.Fatalf("delete: %d", r.Status)
	}
	if r := e.do(t, "GUEST", "GET", "/api/knowledge/documents/KB-009", ""); r.Status != 404 {
		t.Fatalf("nonaktif untuk guest: %d", r.Status)
	}
	sr = e.do(t, "GUEST", "GET", "/api/knowledge/search?q=cicilan%20nol%20persen&min_score=0.1", "")
	for _, it := range sr.Body["data"].([]any) {
		if it.(map[string]any)["document_id"] == "KB-009" {
			t.Fatal("dokumen nonaktif tidak boleh muncul di pencarian")
		}
	}
	if n := len(e.do(t, "GUEST", "GET", "/api/knowledge/documents?include_inactive=true", "").Body["data"].([]any)); n != 8 {
		t.Fatalf("include_inactive diabaikan untuk guest: %d", n)
	}
	if n := len(e.do(t, "ADMIN", "GET", "/api/knowledge/documents?include_inactive=true", "").Body["data"].([]any)); n != 9 {
		t.Fatalf("admin include_inactive: %d", n)
	}
	// reindex
	if r := e.do(t, "BUDI", "POST", "/api/knowledge/reindex", ""); r.Status != 403 {
		t.Fatalf("customer reindex: %d", r.Status)
	}
	if r := e.do(t, "ADMIN", "POST", "/api/knowledge/reindex", ""); r.Status != 202 || data(r)["queued"].(float64) < 8 {
		t.Fatalf("reindex: %d %v", r.Status, r.Body)
	}
	e2 := setup(t, false) // embedding mati
	if r := e2.do(t, "ADMIN", "POST", "/api/knowledge/reindex", ""); r.Status != 503 || code(r) != "EMBEDDING_UNAVAILABLE" {
		t.Fatalf("reindex tanpa embedding: %d %s", r.Status, code(r))
	}
	// tidak ada kebijakan cicilan di seed (uji negatif agent): pencarian tak menemukan
	e3 := setup(t, true)
	sr = e3.do(t, "GUEST", "GET", "/api/knowledge/search?q=cicilan%20nol%20persen&min_score=0.2", "")
	if len(sr.Body["data"].([]any)) != 0 {
		t.Fatalf("seed tidak boleh memuat kebijakan cicilan: %v", sr.Body["data"])
	}
}
