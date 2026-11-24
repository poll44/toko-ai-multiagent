// Package config: konfigurasi service-order.
package config // nama package: config

import ( // package yang dipakai
	"time" // tipe durasi

	"toko-ai-microservices/internal/shared/config" // helper env bersama
)

// Config menampung seluruh konfigurasi service-order.
type Config struct {
	Port            string        // port HTTP
	AppEnv          string        // mode aplikasi
	InternalAPIKey  string        // kunci internal antar-service
	DBPath          string        // lokasi order.db
	CatalogURL      string        // URL service-catalog
	PaymentTTL      time.Duration // batas waktu pembayaran sebelum pesanan kedaluwarsa
	UpstreamTimeout time.Duration // timeout ke service lain
}

// Load membaca .env dan environment, memvalidasi variabel wajib, lalu mengembalikan Config.
func Load() (*Config, error) {
	config.LoadDotEnv("service-order/.env", ".env", "../.env") // muat file .env sesuai prioritas
	if err := config.Require("INTERNAL_API_KEY"); err != nil { // kunci internal wajib
		return nil, err
	}
	return &Config{
		Port:            config.String("ORDER_PORT", "4002"),                                      // port default 4002
		AppEnv:          config.String("APP_ENV", "development"),                                  // default development
		InternalAPIKey:  config.String("INTERNAL_API_KEY", ""),                                    // kunci internal
		DBPath:          config.String("ORDER_DB_PATH", "./data/order.db"),                        // path database
		CatalogURL:      config.String("CATALOG_URL", "http://localhost:4001"),                    // URL catalog
		PaymentTTL:      time.Duration(config.Int("ORDER_PAYMENT_TTL_MINUTES", 15)) * time.Minute, // TTL pembayaran 15 menit
		UpstreamTimeout: config.Millis("UPSTREAM_TIMEOUT_MS", 10000),                              // timeout 10 detik
	}, nil
}
