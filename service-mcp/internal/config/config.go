// Package config: konfigurasi service-mcp.
package config // nama package: config

import ( // package yang dipakai
	"time" // tipe durasi

	"toko-ai-microservices/internal/shared/config" // helper env bersama
)

// Config menampung seluruh konfigurasi service-mcp.
type Config struct {
	Port            string        // port HTTP
	AppEnv          string        // mode aplikasi
	InternalAPIKey  string        // kunci internal antar-service
	CatalogURL      string        // URL service-catalog
	OrderURL        string        // URL service-order
	TicketURL       string        // URL service-ticket
	UpstreamTimeout time.Duration // timeout ke service domain
	DevUserID       string        // hanya mode --stdio
	DevUserRole     string        // hanya mode --stdio
}

// Load membaca .env dan environment, memvalidasi variabel wajib, lalu mengembalikan Config.
func Load() (*Config, error) {
	config.LoadDotEnv("service-mcp/.env", ".env", "../.env")   // muat file .env sesuai prioritas
	if err := config.Require("INTERNAL_API_KEY"); err != nil { // kunci internal wajib
		return nil, err
	}
	return &Config{
		Port:            config.String("MCP_PORT", "5000"),                     // port default 5000
		AppEnv:          config.String("APP_ENV", "development"),               // default development
		InternalAPIKey:  config.String("INTERNAL_API_KEY", ""),                 // kunci internal
		CatalogURL:      config.String("CATALOG_URL", "http://localhost:4001"), // URL catalog
		OrderURL:        config.String("ORDER_URL", "http://localhost:4002"),   // URL order
		TicketURL:       config.String("TICKET_URL", "http://localhost:4003"),  // URL ticket
		UpstreamTimeout: config.Millis("UPSTREAM_TIMEOUT_MS", 10000),           // timeout 10 detik
		DevUserID:       config.String("MCP_DEV_USER_ID", "GUEST"),             // user dev (stdio)
		DevUserRole:     config.String("MCP_DEV_USER_ROLE", "GUEST"),           // role dev (stdio)
	}, nil
}
