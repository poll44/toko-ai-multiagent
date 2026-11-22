// Package config: konfigurasi service-gateway dari environment (fail-fast).
package config

import (
	"time"

	"toko-ai-microservices/internal/shared/config"
)

type Config struct {
	Port            string
	AppEnv          string
	JWTSecret       string
	JWTExpiresIn    time.Duration
	InternalAPIKey  string
	AuthDBPath      string
	CORSOrigin      string
	CatalogURL      string
	OrderURL        string
	TicketURL       string
	MCPBaseURL      string // untuk GET /health pada /api/dev/health
	AgentURL        string
	UpstreamTimeout time.Duration
	AgentTimeout    time.Duration
}

func Load() (*Config, error) {
	config.LoadDotEnv("service-gateway/.env", ".env", "../.env")
	if err := config.Require("JWT_SECRET", "INTERNAL_API_KEY"); err != nil {
		return nil, err
	}
	return &Config{
		Port:            config.String("GATEWAY_PORT", "8080"),
		AppEnv:          config.String("APP_ENV", "development"),
		JWTSecret:       config.String("JWT_SECRET", ""),
		JWTExpiresIn:    config.Duration("JWT_EXPIRES_IN", 8*time.Hour),
		InternalAPIKey:  config.String("INTERNAL_API_KEY", ""),
		AuthDBPath:      config.String("AUTH_DB_PATH", "./data/auth.db"),
		CORSOrigin:      config.String("CORS_ORIGIN", "http://localhost:3000"),
		CatalogURL:      config.String("CATALOG_URL", "http://localhost:4001"),
		OrderURL:        config.String("ORDER_URL", "http://localhost:4002"),
		TicketURL:       config.String("TICKET_URL", "http://localhost:4003"),
		MCPBaseURL:      config.String("MCP_BASE_URL", "http://localhost:5000"),
		AgentURL:        config.String("AGENT_URL", "http://localhost:8000"),
		UpstreamTimeout: config.Millis("UPSTREAM_TIMEOUT_MS", 10000),
		AgentTimeout:    config.Millis("AGENT_TIMEOUT_MS", 120000),
	}, nil
}
