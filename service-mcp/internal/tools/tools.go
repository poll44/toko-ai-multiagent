// Package tools: 17 MCP Tools + pemetaan ke REST service Go (TRD 05 §3–§4).
// Identitas TIDAK pernah dari argumen tool; selalu dari header transport yang sudah divalidasi.
package tools

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/svcclient"
)

// Clients: klien REST per service (membawa x-internal-api-key, x-service-name: service-mcp).
type Clients struct{ Catalog, Order, Ticket *svcclient.Client }

type args = map[string]any

// def: satu tool. Run mengembalikan status + body JSON upstream apa adanya.
type def struct {
	Name, Description string
	Roles             []string
	Schema            m
	Run               func(ctx context.Context, c *Clients, a args) (int, []byte)
}

var (
	all      = []string{httputil.RoleGuest, httputil.RoleCustomer, httputil.RoleAdminSistem, httputil.RoleAdminDev}
	customer = []string{httputil.RoleCustomer}
	signedIn = []string{httputil.RoleCustomer, httputil.RoleAdminSistem, httputil.RoleAdminDev}
	admin    = []string{httputil.RoleAdminSistem}
)

// ---- util argumen ----

func str(a args, k string) string { s, _ := a[k].(string); return s }

func num(a args, k string) (int, bool) {
	switch v := a[k].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	}
	return 0, false
}

func boolean(a args, k string) (bool, bool) { b, ok := a[k].(bool); return b, ok }

// qs membangun query string dari argumen (nilai kosong dilewati). Pasangan: nama query, nilai.
func qs(kv ...any) string {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		k := kv[i].(string)
		switch x := kv[i+1].(type) {
		case string:
			if x != "" {
				v.Set(k, x)
			}
		case int:
			v.Set(k, strconv.Itoa(x))
		case bool:
			v.Set(k, strconv.FormatBool(x))
		case nil:
		}
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// opt mengembalikan nilai argumen opsional sebagai any (nil bila tidak ada) untuk qs.
func opt(a args, k string) any {
	switch a[k].(type) {
	case string:
		return str(a, k)
	case float64, int, json.Number:
		n, _ := num(a, k)
		return n
	case bool:
		b, _ := boolean(a, k)
		return b
	}
	return nil
}

// ---- definisi tool ----

func definitions() []def {
	pid := strSchema(0, 0, productIDPattern)
	oid := strSchema(0, 0, orderIDPattern)
	tid := strSchema(0, 0, ticketIDPattern)
	cat := enumSchema(categories...)

	return []def{
		// ───── Catalog ─────
		{
			Name: "search_products", Roles: all,
			Description: "Pencarian EKSAK produk: kata kunci literal (nama/merek), kategori, rentang harga, urutan. Gunakan bila pengguna menyebut nama produk, kata kunci jelas, atau filter harga/kategori. Untuk kebutuhan/keluhan/deskripsi bebas gunakan semantic_search_products.",
			Schema: object(m{
				"query": strSchema(0, 100, ""), "category": cat,
				"min_price": intSchema(0, 1_000_000_000), "max_price": intSchema(0, 1_000_000_000),
				"in_stock_only": boolSchema(), "sort": enumSchema("name", "price_asc", "price_desc", "newest"),
				"limit": intDefault(1, 20, 10),
			}),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Catalog.DoRaw(ctx, "GET", "/api/products"+qs("q", opt(a, "query"), "category", opt(a, "category"),
					"min_price", opt(a, "min_price"), "max_price", opt(a, "max_price"), "in_stock", opt(a, "in_stock_only"),
					"sort", opt(a, "sort"), "limit", opt(a, "limit")), nil)
			},
		},
		{
			Name: "semantic_search_products", Roles: all,
			Description: "Pencarian SEMANTIK produk berdasarkan makna kebutuhan, keluhan, atau skenario pemakaian dalam bahasa natural (mis. 'mouse yang tidak bikin pergelangan pegal', 'headphone untuk di kereta yang berisik'). Hasil diurutkan berdasarkan kemiripan (score 0–1). Gunakan bila pengguna TIDAK menyebut nama/ID/filter eksak. Setelah itu verifikasi stok/harga dengan check_stock atau get_product sebelum menawarkan.",
			Schema: object(m{
				"query": strSchema(3, 300, ""), "category": cat,
				"max_price": intSchema(0, 1_000_000_000), "min_price": intSchema(0, 1_000_000_000),
				"in_stock_only": boolSchema(), "limit": intDefault(1, 10, 5),
			}, "query"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Catalog.DoRaw(ctx, "GET", "/api/products/semantic-search"+qs("q", opt(a, "query"), "category", opt(a, "category"),
					"max_price", opt(a, "max_price"), "min_price", opt(a, "min_price"), "in_stock", opt(a, "in_stock_only"),
					"limit", opt(a, "limit")), nil)
			},
		},
		{
			Name: "get_product", Roles: all, Description: "Ambil detail satu produk berdasarkan ID (mis. PRD-003).",
			Schema: object(m{"product_id": pid}, "product_id"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Catalog.DoRaw(ctx, "GET", "/api/products/"+url.PathEscape(str(a, "product_id")), nil)
			},
		},
		{
			Name: "list_categories", Roles: all, Description: "Daftar kategori produk beserta jumlah produknya.",
			Schema: object(m{}),
			Run: func(ctx context.Context, c *Clients, _ args) (int, []byte) {
				return c.Catalog.DoRaw(ctx, "GET", "/api/products/categories", nil)
			},
		},
		{
			Name: "check_stock", Roles: all, Description: "Cek ketersediaan stok satu produk (stok, stock_status, harga terkini).",
			Schema: object(m{"product_id": pid}, "product_id"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				st, body := c.Catalog.DoRaw(ctx, "GET", "/api/products/"+url.PathEscape(str(a, "product_id")), nil)
				if st >= 400 {
					return st, body
				}
				var env struct {
					Data map[string]any `json:"data"`
				}
				if err := json.Unmarshal(body, &env); err != nil || env.Data == nil {
					return st, body
				}
				out := map[string]any{}
				for _, k := range []string{"id", "name", "stock", "stock_status", "price"} {
					out[k] = env.Data[k]
				}
				b, _ := json.Marshal(map[string]any{"data": out})
				return st, b
			},
		},
		{
			Name: "adjust_product_stock", Roles: admin, Description: "(Admin) Atur stok produk: mode 'set' (stok = value) atau 'adjust' (stok += value).",
			Schema: object(m{"product_id": pid, "mode": enumSchema("set", "adjust"), "value": m{"type": "integer"}, "note": strSchema(0, 200, "")},
				"product_id", "mode", "value"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				v, _ := num(a, "value")
				return c.Catalog.DoRaw(ctx, "PATCH", "/api/products/"+url.PathEscape(str(a, "product_id"))+"/stock",
					m{"mode": str(a, "mode"), "value": v, "note": str(a, "note")})
			},
		},

		// ───── Knowledge base (service-ticket) ─────
		{
			Name: "search_store_policy", Roles: all,
			Description: "Cari kebijakan/FAQ/SOP resmi toko (retur, garansi, estimasi pengiriman, pembayaran gagal/kedaluwarsa, pembatalan, refund, prosedur komplain, jam layanan) secara semantik. WAJIB dipanggil sebelum menjawab pertanyaan kebijakan dan sebelum memutuskan membuat tiket untuk hal yang mungkin sudah diatur kebijakan. Jawab hanya berdasarkan hasil; jika kosong, katakan tidak ada informasi.",
			Schema:      object(m{"query": strSchema(3, 300, ""), "category": enumSchema(kbCategories...), "limit": intDefault(1, 5, 3)}, "query"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Ticket.DoRaw(ctx, "GET", "/api/knowledge/search"+qs("q", opt(a, "query"), "category", opt(a, "category"), "limit", opt(a, "limit")), nil)
			},
		},

		// ───── Order ─────
		{
			Name: "create_order", Roles: customer,
			Description: "Buat pesanan baru. HANYA panggil setelah pelanggan secara eksplisit mengonfirmasi produk, jumlah, metode pembayaran, dan alamat.",
			Schema: object(m{
				"items":             arraySchema(object(m{"product_id": pid, "quantity": intSchema(1, 99)}, "product_id", "quantity"), 1, 20),
				"payment_method":    enumSchema("EWALLET", "BANK_TRANSFER", "CREDIT_CARD"),
				"shipping_address":  strSchema(10, 300, ""),
				"confirmed_by_user": constTrue(),
			}, "items", "payment_method", "shipping_address", "confirmed_by_user"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Order.DoRaw(ctx, "POST", "/api/orders",
					m{"items": a["items"], "payment_method": a["payment_method"], "shipping_address": a["shipping_address"]})
			},
		},
		{
			Name: "list_my_orders", Roles: signedIn, Description: "Daftar pesanan milik user yang sedang login (admin: semua).",
			Schema: object(m{"status": enumSchema(orderStatuses...), "limit": intDefault(1, 20, 10)}),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Order.DoRaw(ctx, "GET", "/api/orders"+qs("status", opt(a, "status"), "limit", opt(a, "limit")), nil)
			},
		},
		{
			Name: "get_order", Roles: signedIn, Description: "Detail pesanan: status, status pembayaran, alasan gagal (failure_reason), resi, item.",
			Schema: object(m{"order_id": oid}, "order_id"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Order.DoRaw(ctx, "GET", "/api/orders/"+url.PathEscape(str(a, "order_id")), nil)
			},
		},
		{
			Name: "pay_order", Roles: customer, Description: "Proses pembayaran (simulasi sukses) untuk pesanan PENDING_PAYMENT milik user. Butuh konfirmasi eksplisit.",
			Schema: object(m{"order_id": oid, "confirmed_by_user": constTrue()}, "order_id", "confirmed_by_user"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Order.DoRaw(ctx, "POST", "/api/orders/"+url.PathEscape(str(a, "order_id"))+"/pay", m{"simulate": "success"})
			},
		},
		{
			Name: "cancel_order", Roles: customer, Description: "Batalkan pesanan PENDING_PAYMENT milik user. Butuh konfirmasi eksplisit.",
			Schema: object(m{"order_id": oid, "reason": strSchema(0, 200, ""), "confirmed_by_user": constTrue()}, "order_id", "confirmed_by_user"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Order.DoRaw(ctx, "POST", "/api/orders/"+url.PathEscape(str(a, "order_id"))+"/cancel", m{"reason": str(a, "reason")})
			},
		},

		// ───── Ticket ─────
		{
			Name: "create_ticket", Roles: customer, Description: "Buat tiket komplain/bantuan. Panggil setelah pelanggan mengonfirmasi ringkasan tiket.",
			Schema: object(m{
				"subject": strSchema(5, 120, ""), "description": strSchema(10, 2000, ""),
				"category":          enumSchema("PAYMENT", "SHIPPING", "PRODUCT", "TECHNICAL", "OTHER"),
				"priority":          enumDefault("MEDIUM", "LOW", "MEDIUM", "HIGH"),
				"order_id":          oid,
				"confirmed_by_user": constTrue(),
			}, "subject", "description", "category", "confirmed_by_user"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				body := m{"subject": a["subject"], "description": a["description"], "category": a["category"], "priority": a["priority"]}
				if v := str(a, "order_id"); v != "" {
					body["order_id"] = v
				}
				return c.Ticket.DoRaw(ctx, "POST", "/api/tickets", body)
			},
		},
		{
			Name: "list_my_tickets", Roles: signedIn, Description: "Daftar tiket milik user yang sedang login (admin: semua).",
			Schema: object(m{"status": enumSchema(ticketStates...), "order_id": oid, "limit": intDefault(1, 20, 10)}),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Ticket.DoRaw(ctx, "GET", "/api/tickets"+qs("status", opt(a, "status"), "order_id", opt(a, "order_id"), "limit", opt(a, "limit")), nil)
			},
		},
		{
			Name: "get_ticket", Roles: signedIn, Description: "Detail tiket beserta percakapan dan riwayat status.",
			Schema: object(m{"ticket_id": tid}, "ticket_id"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Ticket.DoRaw(ctx, "GET", "/api/tickets/"+url.PathEscape(str(a, "ticket_id")), nil)
			},
		},
		{
			Name: "add_ticket_message", Roles: signedIn, Description: "Tambahkan pesan ke tiket.",
			Schema: object(m{"ticket_id": tid, "message": strSchema(1, 2000, "")}, "ticket_id", "message"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				return c.Ticket.DoRaw(ctx, "POST", "/api/tickets/"+url.PathEscape(str(a, "ticket_id"))+"/messages", m{"message": str(a, "message")})
			},
		},
		{
			Name: "update_ticket_status", Roles: signedIn,
			Description: "Ubah status tiket sesuai lifecycle (customer: reopen/close; admin: proses & selesaikan).",
			Schema: object(m{"ticket_id": tid, "status": enumSchema(ticketStates...),
				"resolution_note": strSchema(10, 500, ""), "message": strSchema(0, 2000, "")}, "ticket_id", "status"),
			Run: func(ctx context.Context, c *Clients, a args) (int, []byte) {
				body := m{"status": str(a, "status")}
				if v := str(a, "resolution_note"); v != "" {
					body["resolution_note"] = v
				}
				if v := str(a, "message"); v != "" {
					body["message"] = v
				}
				return c.Ticket.DoRaw(ctx, "PATCH", "/api/tickets/"+url.PathEscape(str(a, "ticket_id")), body)
			},
		},
	}
}

// Names: semua nama tool (urutan definisi).
func Names() []string {
	var n []string
	for _, d := range definitions() {
		n = append(n, d.Name)
	}
	return n
}

// NewServer membangun mcp.Server untuk SATU identitas: hanya tool yang terlihat oleh role pemanggil.
// Identitas ditangkap lewat closure (bukan dari argumen tool) dan diteruskan ke service domain.
func NewServer(id httputil.Identity, c *Clients) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "service-mcp", Version: "0.1.0"}, nil)
	for _, d := range definitions() {
		if !slices.Contains(d.Roles, id.Role) {
			continue
		}
		d := d
		tool := &mcp.Tool{Name: d.Name, Description: d.Description, InputSchema: compile(d.Schema)}
		mcp.AddTool(srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in args) (*mcp.CallToolResult, any, error) {
			ctx = httputil.WithIdentity(ctx, id)
			status, body := d.Run(ctx, c, in)
			// Body REST diteruskan APA ADANYA ({data,meta} atau {error}); status >= 400 => IsError.
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
				IsError: status >= 400,
			}, nil, nil
		})
	}
	return srv
}
