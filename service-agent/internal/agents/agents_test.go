package agents // test internal untuk package agents

import ( // package yang dipakai
	"context"           // context
	"iter"              // iterator Seq2
	"net/http"          // tipe HTTP
	"net/http/httptest" // server uji
	"sort"              // pengurutan
	"sync"              // mutex
	"testing"           // framework test

	"github.com/modelcontextprotocol/go-sdk/mcp" // SDK MCP (untuk stub server)

	"google.golang.org/adk/model"   // tipe LLM
	"google.golang.org/adk/session" // tipe session state
	"google.golang.org/genai"       // tipe konten GenAI
)

// ---- fakes ----

// fakeState: implementasi sederhana session.ReadonlyState berbasis map.
type fakeState map[string]any

// Get mengambil nilai kunci k, atau ErrStateKeyNotExist bila tidak ada.
func (f fakeState) Get(k string) (any, error) {
	if v, ok := f[k]; ok {
		return v, nil
	}
	return nil, session.ErrStateKeyNotExist
}

// All mengiterasi seluruh pasangan kunci-nilai.
func (f fakeState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range f {
			if !yield(k, v) {
				return
			}
		}
	}
}

// fakeCtx: konteks agent palsu yang hanya menyediakan state & identitas tetap.
type fakeCtx struct {
	context.Context           // context dasar
	state           fakeState // session state palsu
}

func (c fakeCtx) UserContent() *genai.Content          { return nil }                 // tanpa pesan user
func (c fakeCtx) InvocationID() string                 { return "inv-1" }             // ID invocation tetap
func (c fakeCtx) AgentName() string                    { return "test" }              // nama agent
func (c fakeCtx) ReadonlyState() session.ReadonlyState { return c.state }             // state palsu
func (c fakeCtx) UserID() string                       { return "USR-01" }            // user id tetap
func (c fakeCtx) AppName() string                      { return "toko_orchestrator" } // nama app
func (c fakeCtx) SessionID() string                    { return "s-1" }               // session id tetap
func (c fakeCtx) Branch() string                       { return "" }                  // tanpa branch

// fakeLLM: LLM palsu yang tidak menghasilkan apa pun (hanya untuk merakit agent).
type fakeLLM struct{}

// Name mengembalikan nama LLM palsu.
func (fakeLLM) Name() string { return "fake" }

// GenerateContent tidak menghasilkan respons.
func (fakeLLM) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {}
}

// mcpStub: service-mcp palsu (stateless) yang merekam header dan menyediakan 3 tool.
type mcpStub struct {
	mu   sync.Mutex    // melindungi seen
	seen []http.Header // header tiap request yang diterima
}

// newMCPStub menjalankan server MCP palsu dan mengembalikan server + perekam header.
func newMCPStub(t *testing.T) (*httptest.Server, *mcpStub) {
	st := &mcpStub{}
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		st.mu.Lock()
		st.seen = append(st.seen, r.Header.Clone()) // rekam header request
		st.mu.Unlock()
		srv := mcp.NewServer(&mcp.Implementation{Name: "stub", Version: "0"}, nil)
		for _, name := range []string{"get_product", "create_order", "adjust_product_stock"} { // tiga tool palsu
			mcp.AddTool(srv, &mcp.Tool{Name: name, Description: name}, func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "{}"}}}, nil, nil
			})
		}
		return srv
	}, &mcp.StreamableHTTPOptions{Stateless: true})
	srv := httptest.NewServer(h) // jalankan server uji
	t.Cleanup(srv.Close)         // matikan saat test selesai
	return srv, st
}

// headers mengembalikan nilai header `name` dari semua request yang terekam.
func (s *mcpStub) headers(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, h := range s.seen {
		out = append(out, h.Get(name))
	}
	return out
}

// toolNames mengambil nama tool dari toolset (terurut).
func toolNames(t *testing.T, ts *identityToolset, ctx fakeCtx) []string {
	t.Helper()
	tools, err := ts.Tools(ctx) // ambil daftar tool
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

// TestIdentityFromState: identitas dibaca dari state; tak valid/hilang => GUEST.
func TestIdentityFromState(t *testing.T) {
	cases := []struct { // tabel kasus uji
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
	if u, r := IdentityFromState(nil); u != "GUEST" || r != "GUEST" { // state nil
		t.Errorf("nil state: %s/%s", u, r)
	}
}

// TestToolsetInjectsPerIdentityHeadersAndFilters: header identitas per user harus benar & allowlist tool berlaku.
func TestToolsetInjectsPerIdentityHeadersAndFilters(t *testing.T) {
	srv, stub := newMCPStub(t)
	ts := newIdentityToolset("mcp_test", srv.URL, "internal-k", []string{"get_product", "create_order"}) // adjust_product_stock TIDAK di-allow

	budi := fakeCtx{context.Background(), fakeState{"user_id": "USR-01", "user_role": "CUSTOMER"}} // identitas Budi
	siti := fakeCtx{context.Background(), fakeState{"user_id": "USR-02", "user_role": "CUSTOMER"}} // identitas Siti
	guest := fakeCtx{context.Background(), fakeState{}}                                            // tamu

	if got := toolNames(t, ts, budi); len(got) != 2 || got[0] != "create_order" || got[1] != "get_product" { // tool disaring allowlist
		t.Fatalf("allowlist harus menyaring adjust_product_stock: %v", got)
	}
	toolNames(t, ts, siti)
	toolNames(t, ts, guest)
	toolNames(t, ts, budi) // dari cache (identitas sama)

	pairs := map[string]bool{} // pasangan id/role yang terlihat oleh stub
	ids, roles, keys, reqIDs := stub.headers("x-user-id"), stub.headers("x-user-role"), stub.headers("x-internal-api-key"), stub.headers("x-request-id")
	for i := range ids {
		pairs[ids[i]+"/"+roles[i]] = true
		if keys[i] != "internal-k" { // kunci internal harus ikut
			t.Errorf("x-internal-api-key request %d = %q", i, keys[i])
		}
		if reqIDs[i] == "" { // request id harus ada
			t.Errorf("x-request-id kosong pada request %d", i)
		}
	}
	for _, want := range []string{"USR-01/CUSTOMER", "USR-02/CUSTOMER", "GUEST/GUEST"} { // tiap identitas harus muncul
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

// TestBuildAgentsTree: struktur agent = orchestrator + 2 sub-agent dengan nama yang benar.
func TestBuildAgentsTree(t *testing.T) {
	root, err := BuildWith(fakeLLM{}, "http://localhost:5000/mcp", "k")
	if err != nil {
		t.Fatal(err)
	}
	if root.Name() != "toko_orchestrator" { // nama app ADK
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

// TestInstructionProviderFillsRole: placeholder {user_role} harus terisi dari state.
func TestInstructionProviderFillsRole(t *testing.T) {
	for name, tmpl := range map[string]string{"orchestrator": orchestratorInstruction, "shopping": shoppingInstruction, "support": supportInstruction} {
		out, err := roleProvider(tmpl)(fakeCtx{context.Background(), fakeState{"user_role": "CUSTOMER"}})
		if err != nil {
			t.Fatal(err)
		}
		if !contains(out, "user_role saat ini = CUSTOMER") || contains(out, "{user_role}") { // placeholder harus terganti
			t.Errorf("%s: placeholder role tidak terisi", name)
		}
	}
	out, _ := roleProvider(orchestratorInstruction)(fakeCtx{context.Background(), fakeState{}})
	if !contains(out, "user_role saat ini = GUEST") { // tanpa state => GUEST
		t.Error("tanpa state harus GUEST")
	}
	// prompt memuat kebijakan kunci PRD
	for _, must := range []string{"search_store_policy", "semantic_search_products", "confirmed_by_user"} {
		if !contains(shoppingInstruction+supportInstruction, must) {
			t.Errorf("instruksi harus menyebut %s", must)
		}
	}
}

// TestEntityIDRegex: regex ID entitas hanya cocok untuk format PRD/ORD/TKT/KB berangka >= 3 digit.
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

// contains: true bila sub (tidak kosong) ada di dalam s.
func contains(s, sub string) bool {
	return len(sub) > 0 && (len(s) >= len(sub)) && indexOf(s, sub) >= 0
}

// indexOf mengembalikan posisi pertama sub di s, atau -1.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ { // geser jendela sepanjang sub
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestGuardAnswerH4: klaim aksi sukses harus didukung tool sukses pada giliran itu.
func TestGuardAnswerH4(t *testing.T) {
	ok := map[string]bool{"create_order": true} // tool sukses
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

// TestGuardAnswerH11: jawaban bernada kebijakan wajib bersumber dari search_store_policy.
func TestGuardAnswerH11(t *testing.T) {
	text := "Barang bisa diretur dalam 7 hari. Syarat retur: segel utuh."
	if guardAnswer(text, nil, false) != msgNoPolicy { // tanpa sumber => diganti
		t.Error("kebijakan tanpa search_store_policy harus diganti")
	}
	if guardAnswer(text, map[string]bool{"search_store_policy": true}, false) != "" { // ada tool kebijakan => lolos
		t.Error("dengan search_store_policy sukses harus lolos")
	}
	if guardAnswer(text, nil, true) != "" { // kebijakan pernah ditemukan di sesi => lolos
		t.Error("policy_seen sesi harus lolos")
	}
	if guardAnswer("Produk ini bergaransi: garansi 12 bulan.", map[string]bool{"get_product": true}, false) != "" { // garansi dari data produk
		t.Error("garansi produk dari data produk harus lolos")
	}
	if guardAnswer("Mouse ini harganya Rp150.000 dan tersedia.", nil, false) != "" { // bukan jawaban kebijakan
		t.Error("jawaban non-kebijakan tidak boleh diblokir")
	}
}
