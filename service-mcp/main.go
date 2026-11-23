// service-mcp (:5000) — MCP Tools untuk catalog/order/ticket. Lihat docs/trd/05-SERVICE-MCP.md
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"toko-ai-microservices/internal/shared/httputil"
	"toko-ai-microservices/internal/shared/svcclient"
	"toko-ai-microservices/service-mcp/internal/config"
	"toko-ai-microservices/service-mcp/internal/tools"
)

var userIDRe = regexp.MustCompile(`^((USR|ADM|DEV)-\d{2,}|GUEST)$`)

// strictUserID: format x-user-id harus dikenal (TRD 05 §2.1) selain validasi role oleh IdentityMiddleware.
func strictUserID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !userIDRe.MatchString(httputil.IdentityFrom(r.Context()).UserID) {
			httputil.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "Header x-user-id tidak valid.", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newClients(cfg *config.Config) *tools.Clients {
	mk := func(base string) *svcclient.Client {
		return svcclient.New(base, cfg.InternalAPIKey, "service-mcp", cfg.UpstreamTimeout)
	}
	return &tools.Clients{Catalog: mk(cfg.CatalogURL), Order: mk(cfg.OrderURL), Ticket: mk(cfg.TicketURL)}
}

// newHandler: HTTP handler /mcp (Streamable HTTP, STATELESS): satu mcp.Server per request, identitas dari header
// pada SETIAP request — tidak ada sesi MCP yang dapat dibajak lintas user.
func newHandler(internalKey string, clients *tools.Clients) http.Handler {
	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return tools.NewServer(httputil.IdentityFrom(r.Context()), clients)
	}, &mcp.StreamableHTTPOptions{Stateless: true})

	secured := httputil.Chain(httputil.InternalAuthMiddleware(internalKey), httputil.IdentityMiddleware(), strictUserID)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", httputil.Health("service-mcp"))
	mux.Handle("/mcp", secured(mcpHandler))
	return httputil.Chain(httputil.Recover, httputil.RequestID, httputil.Logging)(mux)
}

func main() {
	stdio := flag.Bool("stdio", false, "mode stdio (DEV saja): identitas dari MCP_DEV_USER_ID/MCP_DEV_USER_ROLE")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("konfigurasi tidak valid", "err", err)
		os.Exit(1)
	}
	httputil.InitLogger(cfg.AppEnv)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	clients := newClients(cfg)

	if *stdio {
		if cfg.AppEnv == "production" {
			slog.Error("mode --stdio dilarang saat APP_ENV=production")
			os.Exit(1)
		}
		if !httputil.ValidRole(cfg.DevUserRole) || !userIDRe.MatchString(cfg.DevUserID) {
			slog.Error("MCP_DEV_USER_ID/MCP_DEV_USER_ROLE tidak valid")
			os.Exit(1)
		}
		id := httputil.Identity{UserID: cfg.DevUserID, Role: cfg.DevUserRole, RequestID: httputil.NewUUID()}
		if err := tools.NewServer(id, clients).Run(ctx, &mcp.StdioTransport{}); err != nil {
			slog.Error("stdio berhenti", "err", err)
			os.Exit(1)
		}
		return
	}

	if err := httputil.RunServer(ctx, "service-mcp", ":"+cfg.Port, newHandler(cfg.InternalAPIKey, clients)); err != nil {
		slog.Error("server berhenti dengan error", "err", err)
		os.Exit(1)
	}
}
