// Package config: konfigurasi service-agent.
package config // nama package: config

import ( // package yang dipakai
	"toko-ai-microservices/internal/shared/config" // helper env bersama
)

// AppName adalah nama aplikasi ADK = nama root agent (dipakai di path /apps/toko_orchestrator/...).
const AppName = "toko_orchestrator"

// Config menampung seluruh konfigurasi service-agent.
type Config struct {
	Port           string // port HTTP
	AppEnv         string // mode aplikasi (development/production)
	GoogleAPIKey   string // API key Gemini untuk LLM
	Model          string // nama model LLM
	MCPURL         string // URL endpoint service-mcp
	InternalAPIKey string // kunci internal antar-service
}

// Load membaca .env dan environment, memvalidasi variabel wajib, lalu mengembalikan Config.
func Load() (*Config, error) {
	config.LoadDotEnv("service-agent/.env", ".env", "../.env") // muat file .env sesuai prioritas
	// GOOGLE_API_KEY wajib (fail-fast): agent tidak berguna tanpa LLM.
	if err := config.Require("INTERNAL_API_KEY", "GOOGLE_API_KEY"); err != nil { // cek variabel wajib
		return nil, err
	}
	return &Config{
		Port:           config.String("AGENT_PORT", "8000"),                   // port default 8000
		AppEnv:         config.String("APP_ENV", "development"),               // default mode development
		GoogleAPIKey:   config.String("GOOGLE_API_KEY", ""),                   // API key Gemini
		Model:          config.String("AGENT_MODEL", "gemini-3.1-flash-lite"), // model default
		MCPURL:         config.String("MCP_URL", "http://localhost:5000/mcp"), // URL MCP default
		InternalAPIKey: config.String("INTERNAL_API_KEY", ""),                 // kunci internal
	}, nil
}
