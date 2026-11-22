// Package config: konfigurasi service-agent.
package config

import (
	"toko-ai-microservices/internal/shared/config"
)

// AppName adalah nama aplikasi ADK = nama root agent (dipakai di path /apps/toko_orchestrator/...).
const AppName = "toko_orchestrator"

type Config struct {
	Port           string
	AppEnv         string
	GoogleAPIKey   string
	Model          string
	MCPURL         string
	InternalAPIKey string
}

func Load() (*Config, error) {
	config.LoadDotEnv("service-agent/.env", ".env", "../.env")
	// GOOGLE_API_KEY wajib (fail-fast): agent tidak berguna tanpa LLM.
	if err := config.Require("INTERNAL_API_KEY", "GOOGLE_API_KEY"); err != nil {
		return nil, err
	}
	return &Config{
		Port:           config.String("AGENT_PORT", "8000"),
		AppEnv:         config.String("APP_ENV", "development"),
		GoogleAPIKey:   config.String("GOOGLE_API_KEY", ""),
		Model:          config.String("AGENT_MODEL", "gemini-flash-lite-latest"),
		MCPURL:         config.String("MCP_URL", "http://localhost:5000/mcp"),
		InternalAPIKey: config.String("INTERNAL_API_KEY", ""),
	}, nil
}
