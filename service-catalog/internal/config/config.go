// Package config: konfigurasi service-catalog.
package config // nama package: config

import ( // package yang dipakai
	"time" // tipe durasi

	"toko-ai-microservices/internal/shared/config" // helper env bersama
)

// Config menampung seluruh konfigurasi service-catalog.
type Config struct {
	Port             string        // port HTTP
	AppEnv           string        // mode aplikasi
	InternalAPIKey   string        // kunci internal antar-service
	DBPath           string        // lokasi file SQLite
	GoogleAPIKey     string        // opsional: kosong => semantic-search memakai keyword_fallback
	EmbeddingModel   string        // nama model embedding
	EmbeddingDim     int           // dimensi vektor
	EmbeddingTimeout time.Duration // batas waktu panggilan embedding
	SemanticMinScore float64       // skor minimum hasil semantik
}

// Load membaca .env dan environment, memvalidasi variabel wajib, lalu mengembalikan Config.
func Load() (*Config, error) {
	config.LoadDotEnv("service-catalog/.env", ".env", "../.env") // muat file .env sesuai prioritas
	if err := config.Require("INTERNAL_API_KEY"); err != nil {   // kunci internal wajib
		return nil, err
	}
	return &Config{
		Port:             config.String("CATALOG_PORT", "4001"),                  // port default 4001
		AppEnv:           config.String("APP_ENV", "development"),                // default development
		InternalAPIKey:   config.String("INTERNAL_API_KEY", ""),                  // kunci internal
		DBPath:           config.String("CATALOG_DB_PATH", "./data/catalog.db"),  // path database
		GoogleAPIKey:     config.String("GOOGLE_API_KEY", ""),                    // API key Gemini (opsional)
		EmbeddingModel:   config.String("EMBEDDING_MODEL", "text-embedding-004"), // model embedding
		EmbeddingDim:     config.Int("EMBEDDING_DIM", 768),                       // dimensi vektor
		EmbeddingTimeout: config.Millis("EMBEDDING_TIMEOUT_MS", 5000),            // timeout embedding (ms)
		SemanticMinScore: config.Float("SEMANTIC_MIN_SCORE", 0.55),               // ambang skor semantik
	}, nil
}
