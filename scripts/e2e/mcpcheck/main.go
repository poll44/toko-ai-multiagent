// Klien MCP (bagian skrip e2e) yang memanggil service-mcp sungguhan.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type rt struct{ h map[string]string }

func (r rt) RoundTrip(q *http.Request) (*http.Response, error) {
	q = q.Clone(q.Context())
	for k, v := range r.h {
		q.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(q)
}

func connect(id, role string) *mcp.ClientSession {
	c := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0"}, nil)
	s, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: "http://localhost:5000/mcp", DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: rt{map[string]string{"x-internal-api-key": "e2e-key", "x-user-id": id, "x-user-role": role}}},
	}, nil)
	if err != nil {
		fmt.Println("FAIL connect", id, err)
		os.Exit(1)
	}
	return s
}

func call(s *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	r, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error(), true
	}
	var b strings.Builder
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String(), r.IsError
}

func main() {
	guest := connect("GUEST", "GUEST")
	tl, _ := guest.ListTools(context.Background(), nil)
	out, isErr := call(guest, "semantic_search_products", map[string]any{"query": "mouse yang tidak bikin pergelangan pegal"})
	stock, _ := call(guest, "check_stock", map[string]any{"product_id": "PRD-014"})
	if len(tl.Tools) == 6 && !isErr && strings.Contains(out, "keyword_fallback") && strings.Contains(stock, `"stock":35`) {
		fmt.Println("PASS guest-tools (6 tool; semantic fallback; check_stock PRD-014=35)")
	} else {
		fmt.Println("FAIL guest-tools", len(tl.Tools), isErr, out, stock)
	}

	budi := connect("USR-01", "CUSTOMER")
	own, e1 := call(budi, "get_order", map[string]any{"order_id": "ORD-003"})
	other, e2 := call(budi, "get_order", map[string]any{"order_id": "ORD-005"})
	if !e1 && strings.Contains(own, "Saldo e-wallet tidak mencukupi") && e2 && strings.Contains(other, "NOT_FOUND") {
		fmt.Println("PASS order-isolation (ORD-003 ok; ORD-005 NOT_FOUND)")
	} else {
		fmt.Println("FAIL order-isolation", own, e1, other, e2)
	}

	before, _ := call(budi, "check_stock", map[string]any{"product_id": "PRD-011"})
	_, noConfirm := call(budi, "create_order", map[string]any{"items": []any{map[string]any{"product_id": "PRD-011", "quantity": 1}}, "payment_method": "EWALLET", "shipping_address": "Jl. Merdeka No. 10, Jakarta"})
	created, e3 := call(budi, "create_order", map[string]any{"items": []any{map[string]any{"product_id": "PRD-011", "quantity": 1}}, "payment_method": "EWALLET", "shipping_address": "Jl. Merdeka No. 10, Jakarta", "confirmed_by_user": true})
	after, _ := call(guest, "check_stock", map[string]any{"product_id": "PRD-011"})
	if noConfirm && !e3 && strings.Contains(created, "PENDING_PAYMENT") && strings.Contains(before, `"stock":150`) && strings.Contains(after, `"stock":149`) {
		fmt.Println("PASS create-order (tanpa konfirmasi ditolak; dengan konfirmasi stok 150 -> 149)")
	} else {
		fmt.Println("FAIL create-order", noConfirm, e3, created, before, after)
	}

	pol, _ := call(guest, "search_store_policy", map[string]any{"query": "pembayaran gagal"})
	if strings.Contains(pol, "KB-005") {
		fmt.Println("PASS policy (search_store_policy menemukan KB-005)")
	} else {
		fmt.Println("FAIL policy", pol)
	}
}
