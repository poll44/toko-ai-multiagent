// Package config: konfigurasi service-catalog.
package config

import (
	"time"

	"toko-ai-microservices/internal/shared/config"
)

type Config struct {
	Port             string
	AppEnv           string
	InternalAPIKey   string
	DBPath           string
	GoogleAPIKey     string // opsional: kosong => semantic-search memakai keyword_fallback
	EmbeddingModel   string
	EmbeddingDim     int
	EmbeddingTimeout time.Duration
	SemanticMinScore float64
}

func Load() (*Config, error) {
	config.LoadDotEnv("service-catalog/.env", ".env", "../.env")
	if err := config.Require("INTERNAL_API_KEY"); err != nil {
		return nil, err
	}
	return &Config{
		Port:             config.String("CATALOG_PORT", "4001"),
		AppEnv:           config.String("APP_ENV", "development"),
		InternalAPIKey:   config.String("INTERNAL_API_KEY", ""),
		DBPath:           config.String("CATALOG_DB_PATH", "./data/catalog.db"),
		GoogleAPIKey:     config.String("GOOGLE_API_KEY", ""),
		EmbeddingModel:   config.String("EMBEDDING_MODEL", "text-embedding-004"),
		EmbeddingDim:     config.Int("EMBEDDING_DIM", 768),
		EmbeddingTimeout: config.Millis("EMBEDDING_TIMEOUT_MS", 5000),
		SemanticMinScore: config.Float("SEMANTIC_MIN_SCORE", 0.55),
	}, nil
}
