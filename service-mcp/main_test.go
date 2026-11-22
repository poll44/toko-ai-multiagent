package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"toko-ai-microservices/internal/shared/svcclient"
	"toko-ai-microservices/service-mcp/internal/tools"
)

const key = "mcp-test-key"

// upstream mencatat request yang diterima service domain palsu.
type upstream struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (u *upstream) last() *http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.reqs) == 0 {
		return nil
	}
	return u.reqs[len(u.reqs)-1]
}

func (u *upstream) count() int { u.mu.Lock(); defer u.mu.Unlock(); return len(u.reqs) }

func newUpstream(t *testing.T) (*httptest.Server, *upstream) {
	u := &upstream{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.reqs = append(u.reqs, r.Clone(context.Background()))
		u.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/orders/ORD-005":
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"Pesanan tidak ditemukan.","details":null,"request_id":"x"}}`))
		case r.URL.Path == "/api/products/semantic-search":
			_, _ = w.Write([]byte(`{"data":[{"id":"PRD-014","score":0.81}],"meta":{"mode":"semantic","total":1}}`))
		case r.URL.Path == "/api/products/PRD-003":
			_, _ = w.Write([]byte(`{"data":{"id":"PRD-003","name":"Powerbank","description":"panjang","stock":80,"stock_status":"IN_STOCK","price":299000}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, u
}

type headerRT struct{ h map[string]string }

func (rt headerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range rt.h {
		r.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func setup(t *testing.T) (mcpURL string, up *upstream) {
	t.Helper()
	us, up := newUpstream(t)
	mk := func() *svcclient.Client { return svcclient.New(us.URL, key, "service-mcp", 2*time.Second) }
	h := newHandler(key, &tools.Clients{Catalog: mk(), Order: mk(), Ticket: mk()})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp", up
}

func connect(t *testing.T, url, userID, role string) *mcp.ClientSession {
	t.Helper()
	hdr := map[string]string{"x-internal-api-key": key, "x-user-id": userID, "x-user-role": role}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: url, HTTPClient: &http.Client{Transport: headerRT{hdr}}, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect %s/%s: %v", userID, role, err)
	}
	t.Cleanup(func() { sess.Close() })
	return sess
}

func toolNames(t *testing.T, s *mcp.ClientSession) []string {
	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, tl := range res.Tools {
		n = append(n, tl.Name)
	}
	return n
}

func text(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	r, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return r
}

func TestAuthRejectedBeforeHandshake(t *testing.T) {
	url, _ := setup(t)
	post := func(hdr map[string]string) int {
		req, _ := http.NewRequest("POST", url, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := post(nil); c != 401 {
		t.Fatalf("tanpa key harus 401, dapat %d", c)
	}
	if c := post(map[string]string{"x-internal-api-key": key, "x-user-id": "USR-01", "x-user-role": "HACKER"}); c != 403 {
		t.Fatalf("role tidak valid harus 403, dapat %d", c)
	}
	if c := post(map[string]string{"x-internal-api-key": key, "x-user-id": "root; drop", "x-user-role": "CUSTOMER"}); c != 400 {
		t.Fatalf("user id tidak valid harus 400, dapat %d", c)
	}
}

func TestToolVisibilityPerRole(t *testing.T) {
	url, _ := setup(t)
	if n := len(tools.Names()); n != 17 {
		t.Fatalf("total tool harus 17, dapat %d", n)
	}
	want := map[string]int{"GUEST": 6, "CUSTOMER": 16, "ADMIN_SISTEM": 13, "ADMIN_DEV": 12}
	ids := map[string]string{"GUEST": "GUEST", "CUSTOMER": "USR-01", "ADMIN_SISTEM": "ADM-01", "ADMIN_DEV": "DEV-01"}
	for role, n := range want {
		names := toolNames(t, connect(t, url, ids[role], role))
		if len(names) != n {
			t.Errorf("%s: harus %d tool, dapat %d: %v", role, n, len(names), names)
		}
		has := func(x string) bool {
			for _, v := range names {
				if v == x {
					return true
				}
			}
			return false
		}
		if !has("semantic_search_products") || !has("search_store_policy") {
			t.Errorf("%s harus melihat semantic_search_products & search_store_policy", role)
		}
		if has("adjust_product_stock") != (role == "ADMIN_SISTEM") {
			t.Errorf("%s: adjust_product_stock hanya untuk ADMIN_SISTEM", role)
		}
		if has("create_order") != (role == "CUSTOMER") {
			t.Errorf("%s: create_order hanya untuk CUSTOMER", role)
		}
	}
}

func TestIdentityForwardedAndErrorPassthrough(t *testing.T) {
	url, up := setup(t)
	s := connect(t, url, "USR-01", "CUSTOMER")

	r := call(t, s, "get_order", map[string]any{"order_id": "ORD-005"})
	if !r.IsError || !strings.Contains(text(r), `"code":"NOT_FOUND"`) {
		t.Fatalf("404 upstream harus jadi isError dengan body error asli: %v %s", r.IsError, text(r))
	}
	req := up.last()
	if req == nil || req.URL.Path != "/api/orders/ORD-005" {
		t.Fatalf("path upstream: %+v", req)
	}
	for h, want := range map[string]string{
		"x-user-id": "USR-01", "x-user-role": "CUSTOMER", "x-internal-api-key": key, "x-service-name": "service-mcp",
	} {
		if got := req.Header.Get(h); got != want {
			t.Errorf("header %s = %q, want %q", h, got, want)
		}
	}
	if req.Header.Get("x-request-id") == "" {
		t.Error("x-request-id harus diteruskan")
	}
}

func TestSemanticAndPolicyPassthrough(t *testing.T) {
	url, up := setup(t)
	s := connect(t, url, "GUEST", "GUEST")

	r := call(t, s, "semantic_search_products", map[string]any{"query": "mouse yang tidak bikin pergelangan pegal"})
	if r.IsError {
		t.Fatalf("seharusnya sukses: %s", text(r))
	}
	if !strings.Contains(text(r), `"mode":"semantic"`) || !strings.Contains(text(r), `"score":0.81`) {
		t.Fatalf("meta & score harus diteruskan: %s", text(r))
	}
	req := up.last()
	if req.URL.Path != "/api/products/semantic-search" || req.URL.Query().Get("q") != "mouse yang tidak bikin pergelangan pegal" {
		t.Fatalf("mapping q: %s", req.URL.String())
	}
	if req.URL.Query().Get("limit") != "5" {
		t.Fatalf("default limit 5 harus diterapkan: %s", req.URL.RawQuery)
	}

	call(t, s, "search_store_policy", map[string]any{"query": "syarat retur barang rusak", "category": "RETURN"})
	req = up.last()
	if req.URL.Path != "/api/knowledge/search" || req.URL.Query().Get("category") != "RETURN" || req.URL.Query().Get("limit") != "3" {
		t.Fatalf("mapping policy: %s", req.URL.String())
	}

	// check_stock hanya mengembalikan 5 field
	r = call(t, s, "check_stock", map[string]any{"product_id": "PRD-003"})
	if strings.Contains(text(r), "description") || !strings.Contains(text(r), `"stock":80`) {
		t.Fatalf("check_stock: %s", text(r))
	}
}

func TestInputValidationRejectedBeforeUpstream(t *testing.T) {
	url, up := setup(t)
	cust := connect(t, url, "USR-01", "CUSTOMER")
	before := up.count()

	bad := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"argumen user_id liar (additionalProperties=false)", "get_order", map[string]any{"order_id": "ORD-001", "user_id": "USR-02"}},
		{"format order_id salah", "get_order", map[string]any{"order_id": "ORD-1; DROP"}},
		{"create_order tanpa confirmed_by_user", "create_order", map[string]any{
			"items": []any{map[string]any{"product_id": "PRD-003", "quantity": 1}}, "payment_method": "EWALLET", "shipping_address": "Jl. Merdeka No. 10, Jakarta"}},
		{"confirmed_by_user=false", "pay_order", map[string]any{"order_id": "ORD-006", "confirmed_by_user": false}},
		{"query semantik < 3 karakter", "semantic_search_products", map[string]any{"query": "ab"}},
		{"limit di luar batas", "search_products", map[string]any{"limit": 999}},
	}
	for _, b := range bad {
		r, err := cust.CallTool(context.Background(), &mcp.CallToolParams{Name: b.tool, Arguments: b.args})
		if err == nil && !r.IsError {
			t.Errorf("%s: harus ditolak", b.name)
		}
	}
	if up.count() != before {
		t.Fatalf("input tidak valid tidak boleh sampai ke upstream (%d -> %d)", before, up.count())
	}

	// tool yang tidak terlihat untuk role => ditolak
	guest := connect(t, url, "GUEST", "GUEST")
	if r, err := guest.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_order", Arguments: map[string]any{}}); err == nil && !r.IsError {
		t.Error("GUEST tidak boleh memanggil create_order")
	}
}

func TestConcurrentIdentitiesDoNotLeak(t *testing.T) {
	url, up := setup(t)
	users := []struct{ id, role string }{{"USR-01", "CUSTOMER"}, {"USR-02", "CUSTOMER"}, {"ADM-01", "ADMIN_SISTEM"}, {"DEV-01", "ADMIN_DEV"}}
	var wg sync.WaitGroup
	for _, u := range users {
		s := connect(t, url, u.id, u.role)
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				call(t, s, "list_my_orders", map[string]any{})
			}
		}(u.id)
	}
	wg.Wait()
	up.mu.Lock()
	defer up.mu.Unlock()
	seen := map[string]int{}
	for _, r := range up.reqs {
		if r.URL.Path == "/api/orders" {
			seen[r.Header.Get("x-user-id")+"/"+r.Header.Get("x-user-role")]++
		}
	}
	for _, u := range users {
		if seen[u.id+"/"+u.role] != 5 {
			t.Errorf("%s/%s harus 5 request dengan identitasnya sendiri, dapat %v", u.id, u.role, seen)
		}
	}
}
