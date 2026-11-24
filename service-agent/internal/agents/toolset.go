package agents // toolset MCP per identitas

import ( // package yang dipakai
	"fmt"      // format error
	"io"       // interface io.Closer
	"net/http" // klien HTTP
	"sync"     // mutex untuk cache
	"time"     // timeout

	"github.com/modelcontextprotocol/go-sdk/mcp" // SDK klien MCP

	"google.golang.org/adk/agent"           // tipe agent/konteks
	"google.golang.org/adk/tool"            // tipe tool
	"google.golang.org/adk/tool/mcptoolset" // toolset berbasis MCP

	"toko-ai-microservices/internal/shared/httputil" // nama header & UUID
)

// maxIdentityToolsets: batas jumlah toolset (identitas) yang di-cache.
const maxIdentityToolsets = 256

// identityToolset adalah tool.Toolset yang memilih koneksi MCP berdasarkan identitas di session state.
// mcptoolset.Config hanya menerima Transport (tanpa header dinamis), sehingga satu mcptoolset dibuat PER
// IDENTITAS (user_id+role) dengan header tetap milik identitas itu. DILARANG memakai satu toolset global
// dengan header statis (TRD 06 §2.1). Toolset di-cache; batas 256 identitas lalu di-reset.
type identityToolset struct {
	name    string   // nama toolset
	mcpURL  string   // URL endpoint MCP
	key     string   // internal API key
	allowed []string // allowlist nama tool untuk agent ini (di atas filter role di service-mcp)

	mu    sync.Mutex              // melindungi cache
	cache map[string]tool.Toolset // cache toolset per "uid|role"
}

// newIdentityToolset membuat toolset dengan cache kosong.
func newIdentityToolset(name, mcpURL, internalKey string, allowed []string) *identityToolset {
	return &identityToolset{name: name, mcpURL: mcpURL, key: internalKey, allowed: allowed, cache: map[string]tool.Toolset{}}
}

// Name mengembalikan nama toolset.
func (t *identityToolset) Name() string { return t.name }

// Tools mengembalikan daftar tool sesuai identitas pemanggil pada session state.
func (t *identityToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	uid, role := IdentityFromState(ctx.ReadonlyState()) // baca identitas dari state
	ts, err := t.forIdentity(uid, role)                 // ambil/buat toolset identitas tsb
	if err != nil {
		return nil, err
	}
	return ts.Tools(ctx) // daftar tool dari toolset itu
}

// forIdentity mengambil toolset dari cache, atau membuatnya (dengan header identitas tetap) bila belum ada.
func (t *identityToolset) forIdentity(uid, role string) (tool.Toolset, error) {
	k := uid + "|" + role         // kunci cache
	t.mu.Lock()                   // kunci akses
	defer t.mu.Unlock()           // lepas saat selesai
	if ts, ok := t.cache[k]; ok { // sudah ada di cache
		return ts, nil
	}
	if len(t.cache) >= maxIdentityToolsets { // cache penuh
		for _, old := range t.cache { // tutup semua koneksi lama
			if c, ok := old.(io.Closer); ok {
				_ = c.Close()
			}
		}
		t.cache = map[string]tool.Toolset{} // reset cache
	}
	transport := &mcp.StreamableClientTransport{ // transport MCP via HTTP
		Endpoint:             t.mcpURL, // alamat service-mcp
		DisableStandaloneSSE: true,     // service-mcp stateless: tidak ada stream server->client
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second, // batas waktu request
			Transport: identityRoundTripper{ // suntik header identitas
				base: http.DefaultTransport,
				headers: map[string]string{
					httputil.HeaderInternalKey: t.key, // kunci internal
					httputil.HeaderUserID:      uid,   // identitas user
					httputil.HeaderUserRole:    role,  // role user
				},
			},
		},
	}
	set, err := mcptoolset.New(mcptoolset.Config{Transport: transport}) // buat toolset MCP
	if err != nil {
		return nil, fmt.Errorf("buat mcp toolset untuk %s: %w", k, err)
	}
	ts := tool.FilterToolset(set, tool.AllowedToolsPredicate(t.allowed)) // batasi ke allowlist
	t.cache[k] = ts                                                      // simpan ke cache
	return ts, nil
}

// identityRoundTripper menyuntik header identitas tetap + x-request-id baru pada tiap request HTTP ke service-mcp.
type identityRoundTripper struct {
	base    http.RoundTripper // transport dasar
	headers map[string]string // header tetap yang disuntikkan
}

// RoundTrip menyalin request, menambahkan header identitas, lalu meneruskannya.
func (rt identityRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())       // salin agar request asli tidak berubah
	for k, v := range rt.headers { // pasang tiap header tetap
		r.Header.Set(k, v)
	}
	r.Header.Set(httputil.HeaderRequestID, httputil.NewUUID()) // request ID baru
	return rt.base.RoundTrip(r)                                // kirim lewat transport dasar
}
