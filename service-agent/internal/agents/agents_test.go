package agents

import (
	"context"
	"iter"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// ---- fakes ----

type fakeState map[string]any

func (f fakeState) Get(k string) (any, error) {
	if v, ok := f[k]; ok {
		return v, nil
	}
	return nil, session.ErrStateKeyNotExist
}
func (f fakeState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range f {
			if !yield(k, v) {
				return
			}
		}
	}
}

type fakeCtx struct {
	context.Context
	state fakeState
}

func (c fakeCtx) UserContent() *genai.Content          { return nil }
func (c fakeCtx) InvocationID() string                 { return "inv-1" }
func (c fakeCtx) AgentName() string                    { return "test" }
func (c fakeCtx) ReadonlyState() session.ReadonlyState { return c.state }
func (c fakeCtx) UserID() string                       { return "USR-01" }
func (c fakeCtx) AppName() string                      { return "toko_orchestrator" }
func (c fakeCtx) SessionID() string                    { return "s-1" }
func (c fakeCtx) Branch() string                       { return "" }

type fakeLLM struct{}

func (fakeLLM) Name() string { return "fake" }
func (fakeLLM) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {}
}

// mcpStub: service-mcp palsu (stateless) yang merekam header dan menyediakan 3 tool.
type mcpStub struct {
	mu   sync.Mutex
	seen []http.Header
}

func newMCPStub(t *testing.T) (*httptest.Server, *mcpStub) {
	st := &mcpStub{}
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		st.mu.Lock()
		st.seen = append(st.seen, r.Header.Clone())
		st.mu.Unlock()
		srv := mcp.NewServer(&mcp.Implementation{Name: "stub", Version: "0"}, nil)
		for _, name := range []string{"get_product", "create_order", "adjust_product_stock"} {
			mcp.AddTool(srv, &mcp.Tool{Name: name, Description: name}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "{}"}}}, nil, nil
			})
		}
		return srv
	}, &mcp.StreamableHTTPOptions{Stateless: true})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, st
}

func (s *mcpStub) headers(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, h := range s.seen {
		out = append(out, h.Get(name))
	}
	return out
}

func toolNames(t *testing.T, ts *identityToolset, ctx fakeCtx) []string {
	t.Helper()
	tools, err := ts.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, tl := range tools {
		n = append(n, tl.Name())
	}
	sort.Strings(n)
	return n
}

// ---- tests ----

func TestIdentityFromState(t *testing.T) {
	cases := []struct {
		name     string
		st       fakeState
		uid, rol string
	}{
		{"lengkap", fakeState{"user_id": "USR-01", "user_role": "CUSTOMER"}, "USR-01", "CUSTOMER"},
		{"kosong => GUEST", fakeState{}, "GUEST", "GUEST"},
		{"role tidak valid => GUEST", fakeState{"user_id": "USR-01", "user_role": "ROOT"}, "USR-01", "GUEST"},
		{"tipe salah => GUEST", fakeState{"user_id": 7, "user_role": true}, "GUEST", "GUEST"},
	}
	for _, c := range cases {
		uid, rol := IdentityFromState(c.st)
		if uid != c.uid || rol != c.rol {
			t.Errorf("%s: got %s/%s want %s/%s", c.name, uid, rol, c.uid, c.rol)
		}
	}
	if u, r := IdentityFromState(nil); u != "GUEST" || r != "GUEST" {
		t.Errorf("nil state: %s/%s", u, r)
	}
}

func TestToolsetInjectsPerIdentityHeadersAndFilters(t *testing.T) {
	srv, stub := newMCPStub(t)
	ts := newIdentityToolset("mcp_test", srv.URL, "internal-k", []string{"get_product", "create_order"}) // adjust_product_stock TIDAK di-allow

	budi := fakeCtx{context.Background(), fakeState{"user_id": "USR-01", "user_role": "CUSTOMER"}}
	siti := fakeCtx{context.Background(), fakeState{"user_id": "USR-02", "user_role": "CUSTOMER"}}
	guest := fakeCtx{context.Background(), fakeState{}}

	if got := toolNames(t, ts, budi); len(got) != 2 || got[0] != "create_order" || got[1] != "get_product" {
		t.Fatalf("allowlist harus menyaring adjust_product_stock: %v", got)
	}
	toolNames(t, ts, siti)
	toolNames(t, ts, guest)
	toolNames(t, ts, budi) // dari cache (identitas sama)

	pairs := map[string]bool{}
	ids, roles, keys, reqIDs := stub.headers("x-user-id"), stub.headers("x-user-role"), stub.headers("x-internal-api-key"), stub.headers("x-request-id")
	for i := range ids {
		pairs[ids[i]+"/"+roles[i]] = true
		if keys[i] != "internal-k" {
			t.Errorf("x-internal-api-key request %d = %q", i, keys[i])
		}
		if reqIDs[i] == "" {
			t.Errorf("x-request-id kosong pada request %d", i)
		}
	}
	for _, want := range []string{"USR-01/CUSTOMER", "USR-02/CUSTOMER", "GUEST/GUEST"} {
		if !pairs[want] {
			t.Errorf("tidak ada request MCP dengan identitas %s (terlihat: %v)", want, pairs)
		}
	}
	// satu identitas TIDAK boleh memakai header identitas lain
	for i := range ids {
		if ids[i] == "USR-02" && roles[i] != "CUSTOMER" {
			t.Errorf("identitas bocor: %s/%s", ids[i], roles[i])
		}
	}
}

func TestBuildAgentsTree(t *testing.T) {
	root, err := BuildWith(fakeLLM{}, "http://localhost:5000/mcp", "k")
	if err != nil {
		t.Fatal(err)
	}
	if root.Name() != "toko_orchestrator" {
		t.Fatalf("nama app ADK harus toko_orchestrator, dapat %s", root.Name())
	}
	var subs []string
	for _, s := range root.SubAgents() {
		subs = append(subs, s.Name())
	}
	sort.Strings(subs)
	if len(subs) != 2 || subs[0] != "order_support_agent" || subs[1] != "shopping_catalog_agent" {
		t.Fatalf("sub-agent: %v", subs)
	}
}

func TestInstructionProviderFillsRole(t *testing.T) {
	for name, tmpl := range map[string]string{"orchestrator": orchestratorInstruction, "shopping": shoppingInstruction, "support": supportInstruction} {
		out, err := roleProvider(tmpl)(fakeCtx{context.Background(), fakeState{"user_role": "CUSTOMER"}})
		if err != nil {
			t.Fatal(err)
		}
		if !contains(out, "user_role saat ini = CUSTOMER") || contains(out, "{user_role}") {
			t.Errorf("%s: placeholder role tidak terisi", name)
		}
	}
	out, _ := roleProvider(orchestratorInstruction)(fakeCtx{context.Background(), fakeState{}})
	if !contains(out, "user_role saat ini = GUEST") {
		t.Error("tanpa state harus GUEST")
	}
	// prompt memuat kebijakan kunci PRD
	for _, must := range []string{"search_store_policy", "semantic_search_products", "confirmed_by_user"} {
		if !contains(shoppingInstruction+supportInstruction, must) {
			t.Errorf("instruksi harus menyebut %s", must)
		}
	}
}

func TestEntityIDRegex(t *testing.T) {
	got := idsIn("Pesanan ORD-003 untuk PRD-014, tiket TKT-001 (KB-005). Bukan ABC-123 atau ORD-1")
	want := []string{"ORD-003", "PRD-014", "TKT-001", "KB-005"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && (len(s) >= len(sub)) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestGuardAnswerH4(t *testing.T) {
	ok := map[string]bool{"create_order": true}
	cases := []struct {
		name, text string
		tools      map[string]bool
		blocked    bool
	}{
		{"klaim dibuat tanpa tool", "Pesanan Anda berhasil dibuat!", nil, true},
		{"klaim dibuat dengan tool", "Pesanan Anda berhasil dibuat!", ok, false},
		{"klaim batal tanpa tool", "Sudah saya batalkan pesanannya.", ok, true},
		{"deskripsi status bukan klaim aksi", "Pesanan ORD-001 sudah dibayar dan sedang dikemas.", nil, false},
		{"tiket dibuat dengan create_ticket", "Tiket berhasil dibuat.", map[string]bool{"create_ticket": true}, false},
	}
	for _, c := range cases {
		if got := guardAnswer(c.text, c.tools, true) != ""; got != c.blocked {
			t.Errorf("%s: blocked=%v want %v", c.name, got, c.blocked)
		}
	}
}

func TestGuardAnswerH11(t *testing.T) {
	text := "Barang bisa diretur dalam 7 hari. Syarat retur: segel utuh."
	if guardAnswer(text, nil, false) != msgNoPolicy {
		t.Error("kebijakan tanpa search_store_policy harus diganti")
	}
	if guardAnswer(text, map[string]bool{"search_store_policy": true}, false) != "" {
		t.Error("dengan search_store_policy sukses harus lolos")
	}
	if guardAnswer(text, nil, true) != "" {
		t.Error("policy_seen sesi harus lolos")
	}
	if guardAnswer("Produk ini bergaransi: garansi 12 bulan.", map[string]bool{"get_product": true}, false) != "" {
		t.Error("garansi produk dari data produk harus lolos")
	}
	if guardAnswer("Mouse ini harganya Rp150.000 dan tersedia.", nil, false) != "" {
		t.Error("jawaban non-kebijakan tidak boleh diblokir")
	}
}
