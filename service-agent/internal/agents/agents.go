package agents

import (
	"context"
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"

	"toko-ai-microservices/service-agent/internal/config"
)

// Allowlist tool per agent (TRD 06 §3.2–3.3). Filter role tetap ditegakkan service-mcp dan service domain.
var (
	shoppingTools = []string{"search_products", "semantic_search_products", "get_product", "list_categories", "check_stock", "search_store_policy"}
	supportTools  = []string{
		"create_order", "list_my_orders", "get_order", "pay_order", "cancel_order",
		"create_ticket", "list_my_tickets", "get_ticket", "add_ticket_message", "update_ticket_status",
		"search_store_policy", "check_stock", "get_product",
	}
)

func temp(t float32) *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{Temperature: &t}
}

// Build membuat model Gemini dari konfigurasi lalu merakit agent.
func Build(ctx context.Context, cfg *config.Config) (agent.Agent, error) {
	llm, err := gemini.NewModel(ctx, cfg.Model, &genai.ClientConfig{APIKey: cfg.GoogleAPIKey})
	if err != nil {
		return nil, fmt.Errorf("buat model %q: %w", cfg.Model, err)
	}
	return BuildWith(llm, cfg.MCPURL, cfg.InternalAPIKey)
}

// BuildWith merakit Orchestrator + Shopping&Catalog + Order&Support dengan LLM yang diberikan (memudahkan test).
func BuildWith(llm model.LLM, mcpURL, internalKey string) (agent.Agent, error) {
	shopping, err := llmagent.New(llmagent.Config{
		Name:                  "shopping_catalog_agent",
		Description:           "Spesialis produk: pencarian eksak & semantik (kebutuhan/keluhan), detail, harga, stok, kategori, rekomendasi, dan kebijakan toko terkait produk.",
		Model:                 llm,
		InstructionProvider:   roleProvider(shoppingInstruction),
		GenerateContentConfig: temp(0.2),
		Toolsets:              []tool.Toolset{newIdentityToolset("mcp_shopping", mcpURL, internalKey, shoppingTools)},
		BeforeAgentCallbacks:  []agent.BeforeAgentCallback{BeforeAgent},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{BeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{AfterTool},
		AfterModelCallbacks:   []llmagent.AfterModelCallback{AfterModel},
	})
	if err != nil {
		return nil, fmt.Errorf("buat shopping_catalog_agent: %w", err)
	}
	support, err := llmagent.New(llmagent.Config{
		Name:                  "order_support_agent",
		Description:           "Spesialis pesanan & layanan pelanggan: membuat/melacak/membayar/membatalkan pesanan, tiket komplain, dan menjawab kebijakan toko (retur, garansi, pengiriman, pembayaran, refund) lewat knowledge base.",
		Model:                 llm,
		InstructionProvider:   roleProvider(supportInstruction),
		GenerateContentConfig: temp(0.2),
		Toolsets:              []tool.Toolset{newIdentityToolset("mcp_support", mcpURL, internalKey, supportTools)},
		BeforeAgentCallbacks:  []agent.BeforeAgentCallback{BeforeAgent},
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{BeforeTool},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{AfterTool},
		AfterModelCallbacks:   []llmagent.AfterModelCallback{AfterModel},
	})
	if err != nil {
		return nil, fmt.Errorf("buat order_support_agent: %w", err)
	}
	root, err := llmagent.New(llmagent.Config{
		Name:                  config.AppName, // nama app ADK = "toko_orchestrator"
		Description:           "Orchestrator Asisten Toko AI: merutekan ke spesialis katalog atau pesanan/dukungan.",
		Model:                 llm,
		InstructionProvider:   roleProvider(orchestratorInstruction),
		GenerateContentConfig: temp(0.3),
		SubAgents:             []agent.Agent{shopping, support},
		BeforeAgentCallbacks:  []agent.BeforeAgentCallback{BeforeAgent},
		AfterModelCallbacks:   []llmagent.AfterModelCallback{AfterModel},
	})
	if err != nil {
		return nil, fmt.Errorf("buat orchestrator: %w", err)
	}
	return root, nil
}
