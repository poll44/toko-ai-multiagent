// Package config: konfigurasi service-mcp.
package config

import (
	"time"

	"toko-ai-microservices/internal/shared/config"
)

type Config struct {
	Port            string
	AppEnv          string
	InternalAPIKey  string
	CatalogURL      string
	OrderURL        string
	TicketURL       string
	UpstreamTimeout time.Duration
	DevUserID       string // hanya mode --stdio
	DevUserRole     string // hanya mode --stdio
}

func Load() (*Config, error) {
	config.LoadDotEnv("service-mcp/.env", ".env", "../.env")
	if err := config.Require("INTERNAL_API_KEY"); err != nil {
		return nil, err
	}
	return &Config{
		Port:            config.String("MCP_PORT", "5000"),
		AppEnv:          config.String("APP_ENV", "development"),
		InternalAPIKey:  config.String("INTERNAL_API_KEY", ""),
		CatalogURL:      config.String("CATALOG_URL", "http://localhost:4001"),
		OrderURL:        config.String("ORDER_URL", "http://localhost:4002"),
		TicketURL:       config.String("TICKET_URL", "http://localhost:4003"),
		UpstreamTimeout: config.Millis("UPSTREAM_TIMEOUT_MS", 10000),
		DevUserID:       config.String("MCP_DEV_USER_ID", "GUEST"),
		DevUserRole:     config.String("MCP_DEV_USER_ROLE", "GUEST"),
	}, nil
}
