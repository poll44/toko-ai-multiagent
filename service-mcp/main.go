// service-mcp (:5000) — MCP Tools untuk catalog/order/ticket. Lihat docs/trd/05-SERVICE-MCP.md
package main // titik masuk program service-mcp

import ( // package yang dipakai
	"context"   // context & pembatalan
	"flag"      // parsing flag command line
	"log/slog"  // logging terstruktur
	"net/http"  // server HTTP
	"os"        // exit
	"os/signal" // menangkap sinyal OS
	"regexp"    // regular expression
	"syscall"   // konstanta SIGTERM

	"github.com/modelcontextprotocol/go-sdk/mcp" // SDK MCP

	"toko-ai-microservices/internal/shared/httputil"    // helper HTTP bersama
	"toko-ai-microservices/internal/shared/svcclient"   // klien service-ke-service
	"toko-ai-microservices/service-mcp/internal/config" // konfigurasi
	"toko-ai-microservices/service-mcp/internal/tools"  // definisi MCP tools
)

// userIDRe: format x-user-id yang dikenal (USR-xx / ADM-xx / DEV-xx / GUEST).
var userIDRe = regexp.MustCompile(`^((USR|ADM|DEV)-\d{2,}|GUEST)$`)

// strictUserID: format x-user-id harus dikenal (TRD 05 §2.1) selain validasi role oleh IdentityMiddleware.
func strictUserID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !userIDRe.MatchString(httputil.IdentityFrom(r.Context()).UserID) { // format user id tidak dikenal
			httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Header x-user-id tidak valid.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// newClients membuat klien REST untuk catalog, order, dan ticket.
func newClients(cfg *config.Config) *tools.Clients {
	mk := func(base string) *svcclient.Client { // pembuat klien dengan identitas "service-mcp"
		return svcclient.New(base, cfg.InternalAPIKey, "service-mcp", cfg.UpstreamTimeout)
	}
	return &tools.Clients{Catalog: mk(cfg.CatalogURL), Order: mk(cfg.OrderURL), Ticket: mk(cfg.TicketURL)}
}

// newHandler: HTTP handler /mcp (Streamable HTTP, STATELESS): satu mcp.Server per request, identitas dari header
// pada SETIAP request — tidak ada sesi MCP yang dapat dibajak lintas user.
func newHandler(internalKey string, clients *tools.Clients) http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { // server baru per request
		return tools.NewServer(httputil.IdentityFrom(r.Context()), clients) // tool sesuai identitas pemanggil
	}, &mcp.StreamableHTTPOptions{Stateless: true}) // mode tanpa sesi

	secured := httputil.Chain(httputil.InternalAuthMiddleware(internalKey), httputil.IdentityMiddleware(), strictUserID) // kunci + identitas + format id
	mux := http.NewServeMux()                                                                                            // router
	mux.HandleFunc("GET /health", httputil.Health("service-mcp"))                                                        // health check tanpa auth
	mux.Handle("/mcp", secured(mcpHandler))                                                                              // endpoint MCP
	return httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)(mux)                                   // middleware global
}

// main: memuat config lalu menjalankan MCP lewat stdio (dev) atau HTTP (default).
func main() {
	stdio := flag.Bool("stdio", false, "mode stdio (DEV saja): identitas dari MCP_DEV_USER_ID/MCP_DEV_USER_ROLE") // flag mode stdio
	flag.Parse()                                                                                                  // baca flag

	cfg, err := config.Load() // baca konfigurasi
	if err != nil {
		slog.Error("konfigurasi tidak valid", "err", err)
		os.Exit(1)
	}
	httputil.InitLogger(cfg.AppEnv)                                                        // pasang logger
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) // batal saat Ctrl+C/SIGTERM
	defer stop()
	clients := newClients(cfg) // klien ke service domain

	if *stdio { // mode stdio (khusus pengembangan)
		if cfg.AppEnv == "production" { // dilarang di produksi
			slog.Error("mode --stdio dilarang saat APP_ENV=production")
			os.Exit(1)
		}
		if !httputil.ValidRole(cfg.DevUserRole) || !userIDRe.MatchString(cfg.DevUserID) { // identitas dev harus valid
			slog.Error("MCP_DEV_USER_ID/MCP_DEV_USER_ROLE tidak valid")
			os.Exit(1)
		}
		id := httputil.Identity{UserID: cfg.DevUserID, Role: cfg.DevUserRole, RequestID: httputil.NewUUID()} // identitas tetap dari env
		if err := tools.NewServer(id, clients).Run(ctx, &mcp.StdioTransport{}); err != nil {                 // jalankan MCP via stdio
			slog.Error("stdio berhenti", "err", err)
			os.Exit(1)
		}
		return
	}

	if err := httputil.RunServer(ctx, "service-mcp", ":"+cfg.Port, newHandler(cfg.InternalAPIKey, clients)); err != nil { // jalankan server HTTP
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
