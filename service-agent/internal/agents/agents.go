package agents // perakitan Orchestrator + sub-agent

import ( // package yang dipakai
	"context" // context
	"fmt"     // format error

	"google.golang.org/adk/agent"          // tipe agent
	"google.golang.org/adk/agent/llmagent" // agent berbasis LLM
	"google.golang.org/adk/model"          // interface LLM
	"google.golang.org/adk/model/gemini"   // model Gemini
	"google.golang.org/adk/tool"           // tipe tool/toolset
	"google.golang.org/genai"              // tipe konfigurasi GenAI

	"toko-ai-microservices/service-agent/internal/config" // konfigurasi service
)

// Allowlist tool per agent (TRD 06 §3.2–3.3). Filter role tetap ditegakkan service-mcp dan service domain.
var (
	// tool yang boleh dipakai agent katalog/belanja
	shoppingTools = []string{"search_products", "semantic_search_products", "get_product", "list_categories", "check_stock", "search_store_policy"}
	// tool yang boleh dipakai agent pesanan & dukungan
	supportTools = []string{
		"create_order", "list_my_orders", "get_order", "pay_order", "cancel_order", // operasi pesanan
		"create_ticket", "list_my_tickets", "get_ticket", "add_ticket_message", "update_ticket_status", // operasi tiket
		"search_store_policy", "check_stock", "get_product", // pendukung: kebijakan & info produk
	}
)

// temp membuat konfigurasi generasi dengan temperature tertentu (makin rendah makin deterministik).
func temp(t float32) *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{Temperature: &t}
}

// Build membuat model Gemini dari konfigurasi lalu merakit agent.
func Build(ctx context.Context, cfg *config.Config) (agent.Agent, error) {
	llm, err := gemini.NewModel(ctx, cfg.Model, &genai.ClientConfig{APIKey: cfg.GoogleAPIKey}) // buat klien model Gemini
	if err != nil {
		return nil, fmt.Errorf("buat model %q: %w", cfg.Model, err)
	}
	return BuildWith(llm, cfg.MCPURL, cfg.InternalAPIKey) // rakit agent dengan LLM tersebut
}

// BuildWith merakit Orchestrator + Shopping&Catalog + Order&Support dengan LLM yang diberikan (memudahkan test).
func BuildWith(llm model.LLM, mcpURL, internalKey string) (agent.Agent, error) {
	shopping, err := llmagent.New(llmagent.Config{ // sub-agent katalog
		Name:                  "shopping_catalog_agent",                                                                                                                           // nama agent
		Description:           "Spesialis produk: pencarian eksak & semantik (kebutuhan/keluhan), detail, harga, stok, kategori, rekomendasi, dan kebijakan toko terkait produk.", // deskripsi untuk router
		Model:                 llm,                                                                                                                                                // LLM yang dipakai
		InstructionProvider:   roleProvider(shoppingInstruction),                                                                                                                  // system prompt dengan role
		GenerateContentConfig: temp(0.2),                                                                                                                                          // temperature rendah
		Toolsets:              []tool.Toolset{newIdentityToolset("mcp_shopping", mcpURL, internalKey, shoppingTools)},                                                             // tool MCP per identitas
		BeforeAgentCallbacks:  []agent.BeforeAgentCallback{BeforeAgent},                                                                                                           // guard sebelum agent jalan
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{BeforeTool},                                                                                                          // guard sebelum tool
		AfterToolCallbacks:    []llmagent.AfterToolCallback{AfterTool},                                                                                                            // pencatatan setelah tool
		AfterModelCallbacks:   []llmagent.AfterModelCallback{AfterModel},                                                                                                          // guard jawaban final
	})
	if err != nil {
		return nil, fmt.Errorf("buat shopping_catalog_agent: %w", err)
	}
	support, err := llmagent.New(llmagent.Config{ // sub-agent pesanan & dukungan
		Name:                  "order_support_agent",                                                                                                                                                                                     // nama agent
		Description:           "Spesialis pesanan & layanan pelanggan: membuat/melacak/membayar/membatalkan pesanan, tiket komplain, dan menjawab kebijakan toko (retur, garansi, pengiriman, pembayaran, refund) lewat knowledge base.", // deskripsi untuk router
		Model:                 llm,                                                                                                                                                                                                       // LLM yang dipakai
		InstructionProvider:   roleProvider(supportInstruction),                                                                                                                                                                          // system prompt dengan role
		GenerateContentConfig: temp(0.2),                                                                                                                                                                                                 // temperature rendah
		Toolsets:              []tool.Toolset{newIdentityToolset("mcp_support", mcpURL, internalKey, supportTools)},                                                                                                                      // tool MCP per identitas
		BeforeAgentCallbacks:  []agent.BeforeAgentCallback{BeforeAgent},                                                                                                                                                                  // guard sebelum agent jalan
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{BeforeTool},                                                                                                                                                                 // guard sebelum tool
		AfterToolCallbacks:    []llmagent.AfterToolCallback{AfterTool},                                                                                                                                                                   // pencatatan setelah tool
		AfterModelCallbacks:   []llmagent.AfterModelCallback{AfterModel},                                                                                                                                                                 // guard jawaban final
	})
	if err != nil {
		return nil, fmt.Errorf("buat order_support_agent: %w", err)
	}
	root, err := llmagent.New(llmagent.Config{ // agent root (router)
		Name:                  config.AppName,                                                                        // nama app ADK = "toko_orchestrator"
		Description:           "Orchestrator Asisten Toko AI: merutekan ke spesialis katalog atau pesanan/dukungan.", // deskripsi
		Model:                 llm,                                                                                   // LLM yang dipakai
		InstructionProvider:   roleProvider(orchestratorInstruction),                                                 // system prompt router
		GenerateContentConfig: temp(0.3),                                                                             // sedikit lebih fleksibel untuk routing
		SubAgents:             []agent.Agent{shopping, support},                                                      // daftar sub-agent
		BeforeAgentCallbacks:  []agent.BeforeAgentCallback{BeforeAgent},                                              // guard sebelum agent jalan
		AfterModelCallbacks:   []llmagent.AfterModelCallback{AfterModel},                                             // guard jawaban final
	})
	if err != nil {
		return nil, fmt.Errorf("buat orchestrator: %w", err)
	}
	return root, nil // kembalikan agent root
}
