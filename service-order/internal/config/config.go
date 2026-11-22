// Package config: konfigurasi service-order.
package config

import (
	"time"

	"toko-ai-microservices/internal/shared/config"
)

type Config struct {
	Port            string
	AppEnv          string
	InternalAPIKey  string
	DBPath          string
	CatalogURL      string
	PaymentTTL      time.Duration
	UpstreamTimeout time.Duration
}

func Load() (*Config, error) {
	config.LoadDotEnv("service-order/.env", ".env", "../.env")
	if err := config.Require("INTERNAL_API_KEY"); err != nil {
		return nil, err
	}
	return &Config{
		Port:            config.String("ORDER_PORT", "4002"),
		AppEnv:          config.String("APP_ENV", "development"),
		InternalAPIKey:  config.String("INTERNAL_API_KEY", ""),
		DBPath:          config.String("ORDER_DB_PATH", "./data/order.db"),
		CatalogURL:      config.String("CATALOG_URL", "http://localhost:4001"),
		PaymentTTL:      time.Duration(config.Int("ORDER_PAYMENT_TTL_MINUTES", 15)) * time.Minute,
		UpstreamTimeout: config.Millis("UPSTREAM_TIMEOUT_MS", 10000),
	}, nil
}
