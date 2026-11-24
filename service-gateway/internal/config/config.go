// Package config: konfigurasi service-gateway dari environment (fail-fast).
package config // nama package: config

import ( // package yang dipakai
	"time" // tipe durasi

	"toko-ai-microservices/internal/shared/config" // helper env bersama
)

// Config menampung seluruh konfigurasi service-gateway.
type Config struct {
	Port            string        // port HTTP
	AppEnv          string        // mode aplikasi
	JWTSecret       string        // secret penandatangan JWT
	JWTExpiresIn    time.Duration // masa berlaku token
	InternalAPIKey  string        // kunci internal antar-service
	AuthDBPath      string        // lokasi auth.db
	CORSOrigin      string        // origin frontend yang diizinkan
	CatalogURL      string        // URL service-catalog
	OrderURL        string        // URL service-order
	TicketURL       string        // URL service-ticket
	MCPBaseURL      string        // untuk GET /health pada /api/dev/health
	AgentURL        string        // URL service-agent
	UpstreamTimeout time.Duration // timeout ke service domain
	AgentTimeout    time.Duration // timeout ke service-agent (lebih lama karena LLM)
}

// Load membaca .env dan environment, memvalidasi variabel wajib, lalu mengembalikan Config.
func Load() (*Config, error) {
	config.LoadDotEnv("service-gateway/.env", ".env", "../.env")             // muat file .env sesuai prioritas
	if err := config.Require("JWT_SECRET", "INTERNAL_API_KEY"); err != nil { // variabel wajib
		return nil, err
	}
	return &Config{
		Port:            config.String("GATEWAY_PORT", "8080"),                  // port default 8080
		AppEnv:          config.String("APP_ENV", "development"),                // default development
		JWTSecret:       config.String("JWT_SECRET", ""),                        // secret JWT
		JWTExpiresIn:    config.Duration("JWT_EXPIRES_IN", 8*time.Hour),         // token berlaku 8 jam
		InternalAPIKey:  config.String("INTERNAL_API_KEY", ""),                  // kunci internal
		AuthDBPath:      config.String("AUTH_DB_PATH", "./data/auth.db"),        // path database
		CORSOrigin:      config.String("CORS_ORIGIN", "http://localhost:3000"),  // origin frontend
		CatalogURL:      config.String("CATALOG_URL", "http://localhost:4001"),  // URL catalog
		OrderURL:        config.String("ORDER_URL", "http://localhost:4002"),    // URL order
		TicketURL:       config.String("TICKET_URL", "http://localhost:4003"),   // URL ticket
		MCPBaseURL:      config.String("MCP_BASE_URL", "http://localhost:5000"), // URL MCP
		AgentURL:        config.String("AGENT_URL", "http://localhost:8000"),    // URL agent
		UpstreamTimeout: config.Millis("UPSTREAM_TIMEOUT_MS", 10000),            // timeout 10 detik
		AgentTimeout:    config.Millis("AGENT_TIMEOUT_MS", 120000),              // timeout agent 120 detik
	}, nil
}
