package agents

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"

	"toko-ai-microservices/internal/shared/httputil"
)

const maxIdentityToolsets = 256

// identityToolset adalah tool.Toolset yang memilih koneksi MCP berdasarkan identitas di session state.
// mcptoolset.Config hanya menerima Transport (tanpa header dinamis), sehingga satu mcptoolset dibuat PER
// IDENTITAS (user_id+role) dengan header tetap milik identitas itu. DILARANG memakai satu toolset global
// dengan header statis (PRD 06 §2.1). Toolset di-cache; batas 256 identitas lalu di-reset.
type identityToolset struct {
	name    string
	mcpURL  string
	key     string
	allowed []string // allowlist nama tool untuk agent ini (di atas filter role di service-mcp)

	mu    sync.Mutex
	cache map[string]tool.Toolset
}

func newIdentityToolset(name, mcpURL, internalKey string, allowed []string) *identityToolset {
	return &identityToolset{name: name, mcpURL: mcpURL, key: internalKey, allowed: allowed, cache: map[string]tool.Toolset{}}
}

func (t *identityToolset) Name() string { return t.name }

func (t *identityToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	uid, role := IdentityFromState(ctx.ReadonlyState())
	ts, err := t.forIdentity(uid, role)
	if err != nil {
		return nil, err
	}
	return ts.Tools(ctx)
}

func (t *identityToolset) forIdentity(uid, role string) (tool.Toolset, error) {
	k := uid + "|" + role
	t.mu.Lock()
	defer t.mu.Unlock()
	if ts, ok := t.cache[k]; ok {
		return ts, nil
	}
	if len(t.cache) >= maxIdentityToolsets {
		for _, old := range t.cache {
			if c, ok := old.(io.Closer); ok {
				_ = c.Close()
			}
		}
		t.cache = map[string]tool.Toolset{}
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:             t.mcpURL,
		DisableStandaloneSSE: true, // service-mcp stateless: tidak ada stream server->client
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: identityRoundTripper{
				base: http.DefaultTransport,
				headers: map[string]string{
					httputil.HeaderInternalKey: t.key,
					httputil.HeaderUserID:      uid,
					httputil.HeaderUserRole:    role,
				},
			},
		},
	}
	set, err := mcptoolset.New(mcptoolset.Config{Transport: transport})
	if err != nil {
		return nil, fmt.Errorf("buat mcp toolset untuk %s: %w", k, err)
	}
	ts := tool.FilterToolset(set, tool.AllowedToolsPredicate(t.allowed))
	t.cache[k] = ts
	return ts, nil
}

// identityRoundTripper menyuntik header identitas tetap + x-request-id baru pada tiap request HTTP ke service-mcp.
type identityRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (rt identityRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range rt.headers {
		r.Header.Set(k, v)
	}
	r.Header.Set(httputil.HeaderRequestID, httputil.NewUUID())
	return rt.base.RoundTrip(r)
}
