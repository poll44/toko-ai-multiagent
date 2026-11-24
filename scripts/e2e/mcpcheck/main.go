// Klien MCP (bagian skrip e2e) yang memanggil service-mcp sungguhan.
package main // titik masuk program pengecekan e2e

import ( // package yang dipakai
	"context"  // context
	"fmt"      // cetak hasil
	"net/http" // transport HTTP
	"os"       // exit
	"strings"  // utilitas string

	"github.com/modelcontextprotocol/go-sdk/mcp" // SDK klien MCP
)

// rt: RoundTripper yang menyuntik header tetap pada setiap request.
type rt struct{ h map[string]string }

// RoundTrip menyalin request, menambahkan header, lalu meneruskannya.
func (r rt) RoundTrip(q *http.Request) (*http.Response, error) {
	q = q.Clone(q.Context()) // salin agar request asli tidak berubah
	for k, v := range r.h {  // pasang tiap header
		q.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(q) // kirim lewat transport bawaan
}

// connect membuka sesi MCP ke service-mcp dengan identitas (id, role) tertentu.
func connect(id, role string) *mcp.ClientSession {
	c := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0"}, nil) // klien MCP
	s, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: "http://localhost:5000/mcp", DisableStandaloneSSE: true, // alamat service-mcp
		HTTPClient: &http.Client{Transport: rt{map[string]string{"x-internal-api-key": "e2e-key", "x-user-id": id, "x-user-role": role}}}, // header identitas
	}, nil)
	if err != nil {
		fmt.Println("FAIL connect", id, err)
		os.Exit(1)
	}
	return s
}

// call memanggil satu tool lalu mengembalikan teks hasil & penanda error.
func call(s *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	r, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args}) // panggil tool
	if err != nil {
		return err.Error(), true
	}
	var b strings.Builder // gabungkan semua konten teks
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String(), r.IsError
}

// main: menjalankan 4 pengecekan e2e (tool tamu, isolasi pesanan, buat pesanan, kebijakan) dan mencetak PASS/FAIL.
func main() {
	guest := connect("GUEST", "GUEST")                                                                                         // sesi tamu
	tl, _ := guest.ListTools(context.Background(), nil)                                                                        // daftar tool untuk tamu
	out, isErr := call(guest, "semantic_search_products", map[string]any{"query": "mouse yang tidak bikin pergelangan pegal"}) // pencarian semantik
	stock, _ := call(guest, "check_stock", map[string]any{"product_id": "PRD-014"})                                            // cek stok PRD-014
	if len(tl.Tools) == 6 && !isErr && strings.Contains(out, "keyword_fallback") && strings.Contains(stock, `"stock":35`) {    // tamu: 6 tool, fallback, stok 35
		fmt.Println("PASS guest-tools (6 tool; semantic fallback; check_stock PRD-014=35)")
	} else {
		fmt.Println("FAIL guest-tools", len(tl.Tools), isErr, out, stock)
	}

	budi := connect("USR-01", "CUSTOMER")                                                                             // sesi pelanggan Budi
	own, e1 := call(budi, "get_order", map[string]any{"order_id": "ORD-003"})                                         // pesanan milik sendiri
	other, e2 := call(budi, "get_order", map[string]any{"order_id": "ORD-005"})                                       // pesanan milik orang lain
	if !e1 && strings.Contains(own, "Saldo e-wallet tidak mencukupi") && e2 && strings.Contains(other, "NOT_FOUND") { // milik sendiri ok, orang lain NOT_FOUND
		fmt.Println("PASS order-isolation (ORD-003 ok; ORD-005 NOT_FOUND)")
	} else {
		fmt.Println("FAIL order-isolation", own, e1, other, e2)
	}

	before, _ := call(budi, "check_stock", map[string]any{"product_id": "PRD-011"})                                                                                                                                                              // stok sebelum pesan
	_, noConfirm := call(budi, "create_order", map[string]any{"items": []any{map[string]any{"product_id": "PRD-011", "quantity": 1}}, "payment_method": "EWALLET", "shipping_address": "Jl. Merdeka No. 10, Jakarta"})                           // tanpa konfirmasi harus ditolak
	created, e3 := call(budi, "create_order", map[string]any{"items": []any{map[string]any{"product_id": "PRD-011", "quantity": 1}}, "payment_method": "EWALLET", "shipping_address": "Jl. Merdeka No. 10, Jakarta", "confirmed_by_user": true}) // dengan konfirmasi
	after, _ := call(guest, "check_stock", map[string]any{"product_id": "PRD-011"})                                                                                                                                                              // stok sesudah pesan
	if noConfirm && !e3 && strings.Contains(created, "PENDING_PAYMENT") && strings.Contains(before, `"stock":150`) && strings.Contains(after, `"stock":149`) {                                                                                   // stok turun 150 -> 149
		fmt.Println("PASS create-order (tanpa konfirmasi ditolak; dengan konfirmasi stok 150 -> 149)")
	} else {
		fmt.Println("FAIL create-order", noConfirm, e3, created, before, after)
	}

	pol, _ := call(guest, "search_store_policy", map[string]any{"query": "pembayaran gagal"}) // cari kebijakan
	if strings.Contains(pol, "KB-005") {                                                      // harus menemukan KB-005
		fmt.Println("PASS policy (search_store_policy menemukan KB-005)")
	} else {
		fmt.Println("FAIL policy", pol)
	}
}
