package handlers_test

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"toko-ai-microservices/internal/shared/embedding"
	"toko-ai-microservices/internal/shared/vector"
	"toko-ai-microservices/service-catalog/internal/handlers"
	"toko-ai-microservices/service-catalog/internal/semantic"
	"toko-ai-microservices/service-catalog/internal/store"
)

const key = "test-key"

// fakeEmbedder: bag-of-words hashing 1024 dimensi — deterministik, tanpa jaringan.
type fakeEmbedder struct{ enabled bool }

var wordRe = regexp.MustCompile(`[a-z0-9]+`)
var stop = map[string]bool{"yang": true, "tidak": true, "dan": true, "untuk": true, "dengan": true, "nama": true, "kategori": true, "deskripsi": true, "spesifikasi": true}

func (f fakeEmbedder) Enabled() bool { return f.enabled }
func (f fakeEmbedder) Model() string { return "fake-bow" }
func (f fakeEmbedder) Dim() int      { return 1024 }
func (f fakeEmbedder) Embed(_ context.Context, text string, _ embedding.Task) ([]float32, error) {
	if !f.enabled {
		return nil, embedding.ErrUnavailable
	}
	v := make([]float32, 1024)
	for _, w := range wordRe.FindAllString(strings.ToLower(text), -1) {
		if stop[w] {
			continue
		}
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%1024]++
	}
	return vector.Normalize(v), nil
}
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

type env struct {
	srv *httptest.Server
	ix  *semantic.Indexer
}

func setup(t *testing.T, embEnabled bool) *env {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := store.NewRepo(db)
	if err := repo.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	ix := semantic.New(repo, fakeEmbedder{enabled: embEnabled})
	if embEnabled {
		if _, err := ix.IndexPending(ctx, false, 0); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	handlers.Register(mux, key, &handlers.Deps{Repo: repo, Index: ix, MinScore: 0.05})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &env{srv: srv, ix: ix}
}

type resp struct {
	Status int
	Body   map[string]any
	Header http.Header
}

func (e *env) do(t *testing.T, method, path, role, body string, hdr ...string) resp {
	t.Helper()
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if role != "NOKEY" {
		req.Header.Set("x-internal-api-key", key)
	}
	ids := map[string]string{"GUEST": "GUEST", "CUSTOMER": "USR-01", "ADMIN_SISTEM": "ADM-01", "ADMIN_DEV": "DEV-01"}
	if id, ok := ids[role]; ok {
		req.Header.Set("x-user-id", id)
		req.Header.Set("x-user-role", role)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
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

func ids(r resp) []string {
	var out []string
	for _, it := range r.Body["data"].([]any) {
		out = append(out, it.(map[string]any)["id"].(string))
	}
	return out
}

func code(r resp) string {
	if e, ok := r.Body["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func stockOf(t *testing.T, e *env, id string) float64 {
	r := e.do(t, "GET", "/api/products/"+id, "GUEST", "")
	return r.Body["data"].(map[string]any)["stock"].(float64)
}

func TestInternalKeyAndHealth(t *testing.T) {
	e := setup(t, false)
	if r := e.do(t, "GET", "/api/products", "NOKEY", ""); r.Status != 401 || code(r) != "INVALID_INTERNAL_KEY" {
		t.Fatalf("tanpa key: %d %s", r.Status, code(r))
	}
	if r := e.do(t, "GET", "/health", "NOKEY", ""); r.Status != 200 {
		t.Fatalf("health: %d", r.Status)
	}
	if r := e.do(t, "GET", "/api/products", "BOGUS", ""); r.Status != 403 {
		t.Fatalf("tanpa identitas: %d", r.Status)
	}
}

func TestListAndExactSearch(t *testing.T) {
	e := setup(t, false)
	r := e.do(t, "GET", "/api/products?limit=100", "GUEST", "")
	if r.Status != 200 || len(ids(r)) != 13 { // 14 seed - 1 nonaktif
		t.Fatalf("guest harus melihat 13 produk aktif, dapat %d", len(ids(r)))
	}
	r = e.do(t, "GET", "/api/products?q=powerbank&max_price=400000", "GUEST", "")
	if got := ids(r); len(got) != 1 || got[0] != "PRD-003" {
		t.Fatalf("q=powerbank: %v", got)
	}
	// LIKE wildcard harus di-escape: "%" tidak boleh mencocokkan semua
	r = e.do(t, "GET", "/api/products?q=%25", "GUEST", "")
	if len(ids(r)) != 0 {
		t.Fatalf("wildcard %% tidak di-escape: %v", ids(r))
	}
	// admin include_inactive; guest diabaikan
	if n := len(ids(e.do(t, "GET", "/api/products?include_inactive=true&limit=100", "ADMIN_SISTEM", ""))); n != 14 {
		t.Fatalf("admin include_inactive: %d", n)
	}
	if n := len(ids(e.do(t, "GET", "/api/products?include_inactive=true&limit=100", "GUEST", ""))); n != 13 {
		t.Fatalf("guest include_inactive harus diabaikan: %d", n)
	}
	if r := e.do(t, "GET", "/api/products/PRD-013", "GUEST", ""); r.Status != 404 {
		t.Fatalf("produk nonaktif untuk guest: %d", r.Status)
	}
	if r := e.do(t, "GET", "/api/products/PRD-013", "ADMIN_DEV", ""); r.Status != 200 {
		t.Fatalf("produk nonaktif untuk admin: %d", r.Status)
	}
	// stock_status
	if s := e.do(t, "GET", "/api/products/PRD-010", "GUEST", "").Body["data"].(map[string]any)["stock_status"]; s != "OUT_OF_STOCK" {
		t.Fatalf("PRD-010: %v", s)
	}
	if s := e.do(t, "GET", "/api/products/PRD-012", "GUEST", "").Body["data"].(map[string]any)["stock_status"]; s != "LOW_STOCK" {
		t.Fatalf("PRD-012: %v", s)
	}
}

func TestSemanticSearch(t *testing.T) {
	e := setup(t, true)
	q := "/api/products/semantic-search?q=" + strings.ReplaceAll("mouse yang tidak bikin pergelangan pegal", " ", "%20")
	r := e.do(t, "GET", q, "GUEST", "")
	if r.Status != 200 {
		t.Fatalf("status %d: %v", r.Status, r.Body)
	}
	got := ids(r)
	if len(got) == 0 || got[0] != "PRD-014" {
		t.Fatalf("PRD-014 harus peringkat 1, dapat %v", got)
	}
	meta := r.Body["meta"].(map[string]any)
	if meta["mode"] != "semantic" {
		t.Fatalf("mode: %v", meta["mode"])
	}
	first := r.Body["data"].([]any)[0].(map[string]any)
	if first["score"] == nil || first["stock"] == nil {
		t.Fatalf("score & stok segar harus ada: %v", first)
	}
	for _, id := range got {
		if id == "PRD-013" {
			t.Fatal("produk nonaktif tidak boleh muncul")
		}
	}
	// filter harga
	r = e.do(t, "GET", q+"&max_price=200000", "GUEST", "")
	for _, id := range ids(r) {
		if id == "PRD-014" {
			t.Fatal("PRD-014 (289000) harus tersaring max_price=200000")
		}
	}
	// validasi
	if r := e.do(t, "GET", "/api/products/semantic-search?q=ab", "GUEST", ""); r.Status != 400 || code(r) != "VALIDATION_ERROR" {
		t.Fatalf("q pendek: %d %s", r.Status, code(r))
	}
	if r := e.do(t, "GET", "/api/products/semantic-search?q=abc&min_score=2", "GUEST", ""); r.Status != 400 {
		t.Fatalf("min_score>1: %d", r.Status)
	}
	// tidak relevan => kosong, 200
	r = e.do(t, "GET", "/api/products/semantic-search?q=resep%20rendang%20padang&min_score=0.5", "GUEST", "")
	if r.Status != 200 || len(ids(r)) != 0 {
		t.Fatalf("query tak relevan harus kosong: %d %v", r.Status, ids(r))
	}
}

func TestSemanticKeywordFallback(t *testing.T) {
	e := setup(t, false) // embedding mati
	r := e.do(t, "GET", "/api/products/semantic-search?q=mouse%20pergelangan", "GUEST", "")
	if r.Status != 200 {
		t.Fatalf("status %d", r.Status)
	}
	if m := r.Body["meta"].(map[string]any)["mode"]; m != "keyword_fallback" {
		t.Fatalf("mode: %v", m)
	}
	if r.Header.Get("x-search-mode") != "keyword_fallback" {
		t.Fatal("header x-search-mode hilang")
	}
	if got := ids(r); len(got) == 0 {
		t.Fatal("fallback harus menemukan produk bermakna 'mouse'/'pergelangan'")
	}
	if first := r.Body["data"].([]any)[0].(map[string]any); first["score"] != nil {
		t.Fatalf("score fallback harus null: %v", first["score"])
	}
}

func TestRBACAndCRUD(t *testing.T) {
	e := setup(t, true)
	body := `{"name":"Mouse Gaming Z1","description":"Mouse gaming ringan","specifications":"DPI 16000","category":"komputer","price":259000,"stock":12}`
	if r := e.do(t, "POST", "/api/products", "CUSTOMER", body); r.Status != 403 {
		t.Fatalf("customer POST: %d", r.Status)
	}
	if r := e.do(t, "POST", "/api/products", "ADMIN_DEV", body); r.Status != 403 {
		t.Fatalf("dev POST: %d", r.Status)
	}
	r := e.do(t, "POST", "/api/products", "ADMIN_SISTEM", body)
	if r.Status != 201 {
		t.Fatalf("admin POST: %d %v", r.Status, r.Body)
	}
	id := r.Body["data"].(map[string]any)["id"].(string)
	if id != "PRD-015" {
		t.Fatalf("id baru: %s", id)
	}
	// otomatis terindeks (embedding sinkron) -> ditemukan semantik
	sr := e.do(t, "GET", "/api/products/semantic-search?q=mouse%20gaming%20ringan&min_score=0.3", "GUEST", "")
	if got := ids(sr); len(got) == 0 || got[0] != id {
		t.Fatalf("produk baru harus langsung dapat dicari semantik: %v", got)
	}
	// validasi
	if r := e.do(t, "POST", "/api/products", "ADMIN_SISTEM", `{"name":"x","category":"BAD CAT","price":-1}`); r.Status != 400 || code(r) != "VALIDATION_ERROR" {
		t.Fatalf("validasi: %d", r.Status)
	}
	// update tidak mengubah stok
	r = e.do(t, "PUT", "/api/products/"+id, "ADMIN_SISTEM", `{"name":"Mouse Gaming Z1 Pro","description":"Mouse gaming ringan","specifications":"DPI 16000","category":"komputer","price":269000,"stock":999}`)
	if r.Status != 200 || r.Body["data"].(map[string]any)["stock"].(float64) != 12 {
		t.Fatalf("update: %d %v", r.Status, r.Body)
	}
	// soft delete => hilang untuk guest
	if r := e.do(t, "DELETE", "/api/products/"+id, "ADMIN_SISTEM", ""); r.Status != 204 {
		t.Fatalf("delete: %d", r.Status)
	}
	if r := e.do(t, "GET", "/api/products/"+id, "GUEST", ""); r.Status != 404 {
		t.Fatalf("setelah delete: %d", r.Status)
	}
	// stok
	r = e.do(t, "PATCH", "/api/products/PRD-003/stock", "ADMIN_SISTEM", `{"mode":"adjust","value":-5,"note":"opname"}`)
	if r.Status != 200 || r.Body["data"].(map[string]any)["stock"].(float64) != 75 {
		t.Fatalf("adjust: %d %v", r.Status, r.Body)
	}
	if r := e.do(t, "PATCH", "/api/products/PRD-003/stock", "ADMIN_SISTEM", `{"mode":"adjust","value":-9999}`); r.Status != 409 || code(r) != "INSUFFICIENT_STOCK" {
		t.Fatalf("stok negatif: %d %s", r.Status, code(r))
	}
	if r := e.do(t, "GET", "/api/products/PRD-003/stock-movements", "CUSTOMER", ""); r.Status != 403 {
		t.Fatalf("movements customer: %d", r.Status)
	}
	if r := e.do(t, "GET", "/api/products/PRD-003/stock-movements", "ADMIN_DEV", ""); r.Status != 200 {
		t.Fatalf("movements dev: %d", r.Status)
	}
}

func TestInternalStock(t *testing.T) {
	e := setup(t, false)
	order := []string{"x-service-name", "service-order"}
	dec := `{"reference_id":"ORD-100","items":[{"product_id":"PRD-003","quantity":1},{"product_id":"PRD-004","quantity":2}]}`

	if r := e.do(t, "POST", "/internal/stock/decrement", "CUSTOMER", dec); r.Status != 403 {
		t.Fatalf("tanpa x-service-name harus 403: %d", r.Status)
	}
	r := e.do(t, "POST", "/internal/stock/decrement", "CUSTOMER", dec, order...)
	if r.Status != 200 {
		t.Fatalf("decrement: %d %v", r.Status, r.Body)
	}
	it := r.Body["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if it["unit_price"].(float64) != 299000 || it["stock_after"].(float64) != 79 {
		t.Fatalf("snapshot harga/stok: %v", it)
	}
	if stockOf(t, e, "PRD-004") != 198 {
		t.Fatal("PRD-004 harus 198")
	}

	// atomik: item kedua kurang stok => item pertama tidak berubah
	before := stockOf(t, e, "PRD-003")
	bad := `{"reference_id":"ORD-101","items":[{"product_id":"PRD-003","quantity":1},{"product_id":"PRD-012","quantity":99}]}`
	r = e.do(t, "POST", "/internal/stock/decrement", "CUSTOMER", bad, order...)
	if r.Status != 409 || code(r) != "INSUFFICIENT_STOCK" {
		t.Fatalf("stok kurang: %d %s", r.Status, code(r))
	}
	if d := r.Body["error"].(map[string]any)["details"].(map[string]any); d["product_id"] != "PRD-012" || d["available"].(float64) != 8 {
		t.Fatalf("details: %v", d)
	}
	if stockOf(t, e, "PRD-003") != before {
		t.Fatal("decrement tidak atomik")
	}
	// produk nonaktif / tidak ada
	if r := e.do(t, "POST", "/internal/stock/decrement", "CUSTOMER", `{"reference_id":"ORD-102","items":[{"product_id":"PRD-013","quantity":1}]}`, order...); r.Status != 422 || code(r) != "PRODUCT_INACTIVE" {
		t.Fatalf("nonaktif: %d %s", r.Status, code(r))
	}
	if r := e.do(t, "POST", "/internal/stock/decrement", "CUSTOMER", `{"reference_id":"ORD-102","items":[{"product_id":"PRD-099","quantity":1}]}`, order...); r.Status != 404 {
		t.Fatalf("tidak ada: %d", r.Status)
	}
	if r := e.do(t, "POST", "/internal/stock/decrement", "CUSTOMER", `{"reference_id":"ORD-102","items":[{"product_id":"PRD-003","quantity":0}]}`, order...); r.Status != 400 {
		t.Fatalf("qty 0: %d", r.Status)
	}

	// restock idempoten
	rs := `{"reference_id":"ORD-100","reason":"PAYMENT_TIMEOUT","items":[{"product_id":"PRD-003","quantity":1},{"product_id":"PRD-004","quantity":2}]}`
	r = e.do(t, "POST", "/internal/stock/restock", "CUSTOMER", rs, order...)
	if r.Status != 200 || r.Body["data"].(map[string]any)["restocked"] != true {
		t.Fatalf("restock pertama: %d %v", r.Status, r.Body)
	}
	r = e.do(t, "POST", "/internal/stock/restock", "CUSTOMER", rs, order...)
	if r.Body["data"].(map[string]any)["restocked"] != false {
		t.Fatalf("restock kedua harus no-op: %v", r.Body)
	}
	if stockOf(t, e, "PRD-003") != 80 || stockOf(t, e, "PRD-004") != 200 {
		t.Fatal("stok setelah restock harus kembali (80, 200)")
	}
}

func TestReindex(t *testing.T) {
	e := setup(t, true)
	if r := e.do(t, "POST", "/api/products/reindex", "CUSTOMER", ""); r.Status != 403 {
		t.Fatalf("customer reindex: %d", r.Status)
	}
	r := e.do(t, "POST", "/api/products/reindex", "ADMIN_SISTEM", "")
	if r.Status != 202 || r.Body["data"].(map[string]any)["queued"].(float64) != 14 {
		t.Fatalf("reindex: %d %v", r.Status, r.Body)
	}
	e2 := setup(t, false)
	if r := e2.do(t, "POST", "/api/products/reindex", "ADMIN_SISTEM", ""); r.Status != 503 || code(r) != "EMBEDDING_UNAVAILABLE" {
		t.Fatalf("reindex tanpa embedding: %d %s", r.Status, code(r))
	}
}
