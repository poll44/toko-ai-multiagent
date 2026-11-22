package agents

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"

	"toko-ai-microservices/internal/shared/httputil"
)

var entityIDRe = regexp.MustCompile(`\b(?:PRD|ORD|TKT|KB)-\d{3,}\b`)

// publicTools: satu-satunya tool yang boleh dipanggil GUEST (PRD 06 §6).
var publicTools = []string{"search_products", "semantic_search_products", "get_product", "list_categories", "check_stock", "search_store_policy"}

// writeTools wajib confirmed_by_user=true (konfirmasi eksplisit 2 langkah, H8).
var writeTools = []string{"create_order", "pay_order", "cancel_order", "create_ticket"}

const (
	msgInvalidSession = "Maaf, sesi percakapan ini tidak valid. Silakan mulai percakapan baru."
	msgUnverified     = "Maaf, saya tidak dapat memverifikasi data tersebut. Mohon ulangi pertanyaan Anda atau sebutkan nomor yang benar."
)

func modelText(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }

func idsIn(s string) []string { return entityIDRe.FindAllString(s, -1) }

func contentText(c *genai.Content) string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range c.Parts {
		if p != nil {
			b.WriteString(p.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// seenIDs membaca himpunan ID yang pernah terlihat di sesi (state string "ID1,ID2").
func seenSet(get func(string) (any, error)) map[string]bool {
	set := map[string]bool{}
	if v, err := get(StateSeenIDs); err == nil {
		if s, ok := v.(string); ok {
			for id := range strings.SplitSeq(s, ",") {
				if id != "" {
					set[id] = true
				}
			}
		}
	}
	return set
}

func addSeen(ctx agent.CallbackContext, ids []string) {
	if len(ids) == 0 {
		return
	}
	set := seenSet(ctx.ReadonlyState().Get)
	for _, id := range ids {
		set[id] = true
	}
	list := make([]string, 0, len(set))
	for id := range set {
		list = append(list, id)
	}
	slices.Sort(list)
	if err := ctx.State().Set(StateSeenIDs, strings.Join(list, ",")); err != nil {
		slog.Warn("gagal menyimpan seen_ids", "err", err)
	}
}

// BeforeAgent: validasi identitas session & catat ID yang disebut user (agar tidak dianggap halusinasi).
// state.user_id harus sama dengan userId invocation — mencegah sesi milik user lain dipakai (PRD 06 §2 aturan 2).
func BeforeAgent(ctx agent.CallbackContext) (*genai.Content, error) {
	if v, err := ctx.ReadonlyState().Get(StateUserID); err == nil {
		if s, _ := v.(string); s != "" && s != ctx.UserID() {
			slog.Warn("identitas session tidak cocok dengan userId", "state_user", s, "user", ctx.UserID())
			return modelText(msgInvalidSession), nil
		}
	}
	if v, err := ctx.ReadonlyState().Get(StateUserRole); err == nil {
		if s, _ := v.(string); s != "" && !httputil.ValidRole(s) {
			return modelText(msgInvalidSession), nil
		}
	}
	startTurn(ctx)
	addSeen(ctx, idsIn(contentText(ctx.UserContent())))
	return nil, nil
}

func blocked(code, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": msg, "details": nil}}
}

// BeforeTool: GUEST hanya tool publik; hapus argumen identitas liar; tool tulis wajib confirmed_by_user=true.
func BeforeTool(ctx agent.ToolContext, t tool.Tool, args map[string]any) (map[string]any, error) {
	_, role := IdentityFromState(ctx.ReadonlyState())
	name := t.Name()
	if role == httputil.RoleGuest && !slices.Contains(publicTools, name) {
		return blocked("FORBIDDEN", "Silakan login terlebih dahulu untuk menggunakan fitur ini."), nil
	}
	for _, k := range []string{"user_id", "userId", "role", "user_role"} {
		delete(args, k) // LLM tidak boleh mengisi identitas
	}
	if slices.Contains(writeTools, name) {
		if ok, _ := args["confirmed_by_user"].(bool); !ok {
			return blocked("CONFIRMATION_REQUIRED",
				"Aksi ini membutuhkan konfirmasi eksplisit dari pelanggan. Tampilkan ringkasan dan tanyakan 'Lanjutkan?' dulu."), nil
		}
	}
	return nil, nil
}

// AfterTool: catat ID entitas dari hasil tool ke seen_ids (H2), tool yang sukses pada giliran ini (H4),
// dan apakah kebijakan resmi pernah ditemukan (H11).
func AfterTool(ctx agent.ToolContext, t tool.Tool, _ map[string]any, result map[string]any, toolErr error) (map[string]any, error) {
	if result == nil {
		return nil, nil
	}
	b, err := json.Marshal(result)
	if err != nil {
		return nil, nil
	}
	addSeen(ctx, idsIn(string(b)))
	if toolErr != nil || result["error"] != nil {
		return nil, nil // gagal: bukan bukti aksi sukses
	}
	st := ctx.State()
	tools := turnTools(ctx.ReadonlyState().Get)
	tools[t.Name()] = true
	if e := st.Set(StateTurnTools, joinSet(tools)); e != nil {
		slog.Warn("gagal menyimpan turn_tools", "err", e)
	}
	if t.Name() == "search_store_policy" && strings.Contains(string(b), `"chunk_id"`) {
		_ = st.Set(StatePolicySeen, "1")
	}
	return nil, nil
}

func joinSet(set map[string]bool) string {
	list := make([]string, 0, len(set))
	for k := range set {
		list = append(list, k)
	}
	slices.Sort(list)
	return strings.Join(list, ",")
}

func turnTools(get func(string) (any, error)) map[string]bool {
	set := map[string]bool{}
	if v, err := get(StateTurnTools); err == nil {
		if s, ok := v.(string); ok {
			for n := range strings.SplitSeq(s, ",") {
				if n != "" {
					set[n] = true
				}
			}
		}
	}
	return set
}

// startTurn mereset pelacakan per giliran bila invocation berganti (sub-agent satu giliran berbagi invocation ID).
func startTurn(ctx agent.CallbackContext) {
	if v, err := ctx.ReadonlyState().Get(StateTurnInv); err == nil {
		if s, _ := v.(string); s == ctx.InvocationID() {
			return
		}
	}
	_ = ctx.State().Set(StateTurnInv, ctx.InvocationID())
	_ = ctx.State().Set(StateTurnTools, "")
}

const (
	msgUnconfirmedAction = "Maaf, saya belum dapat memastikan aksi tersebut berhasil. Silakan periksa menu Pesanan/Tiket, atau minta saya mengecek statusnya."
	msgNoPolicy          = "Maaf, saat ini belum ada informasi resmi mengenai hal tersebut. Anda dapat membuat tiket agar tim kami membantu."
)

// H4: klaim aksi sukses orang-pertama ("berhasil dibuat", "sudah saya batalkan") harus didukung tool sukses pada giliran ini.
var actionClaims = []struct {
	re    *regexp.Regexp
	tools []string
}{
	{regexp.MustCompile(`(?i)\bberhasil\s+(?:saya\s+)?(?:dibuat|dibuatkan|membuat)\b|\b(?:telah|sudah)\s+saya\s+(?:buat|buatkan)\b`), []string{"create_order", "create_ticket"}},
	{regexp.MustCompile(`(?i)\bberhasil\s+(?:saya\s+)?dibatalkan\b|\b(?:telah|sudah)\s+saya\s+batalkan\b`), []string{"cancel_order"}},
	{regexp.MustCompile(`(?i)\bberhasil\s+(?:saya\s+)?(?:dibayar|dibayarkan)\b|\b(?:telah|sudah)\s+saya\s+bayar(?:kan)?\b`), []string{"pay_order"}},
}

// H11: jawaban bernada kebijakan (kata kunci + angka berunit) wajib berasal dari search_store_policy.
var (
	policyKeywordRe = regexp.MustCompile(`(?i)\b(retur|refund|pengembalian dana|estimasi pengiriman|garansi)\b`)
	policyNumberRe  = regexp.MustCompile(`(?i)\b\d+\s*(?:hari|jam|bulan|tahun|%|persen)`)
	productTools    = []string{"get_product", "search_products", "semantic_search_products", "check_stock"}
)

// guardAnswer mengembalikan pesan pengganti bila jawaban melanggar H4/H11, atau "" bila aman.
func guardAnswer(text string, okTools map[string]bool, policySeen bool) string {
	for _, c := range actionClaims {
		if !c.re.MatchString(text) {
			continue
		}
		if !slices.ContainsFunc(c.tools, func(n string) bool { return okTools[n] }) {
			return msgUnconfirmedAction
		}
	}
	if !policySeen && !okTools["search_store_policy"] && policyNumberRe.MatchString(text) {
		kws := policyKeywordRe.FindAllString(text, -1)
		if len(kws) > 0 {
			onlyWarranty := true
			for _, k := range kws {
				if !strings.EqualFold(k, "garansi") {
					onlyWarranty = false
				}
			}
			// garansi produk boleh bersumber dari data produk (spesifikasi)
			if !(onlyWarranty && slices.ContainsFunc(productTools, func(n string) bool { return okTools[n] })) {
				return msgNoPolicy
			}
		}
	}
	return ""
}

// AfterModel (H2): jawaban final tidak boleh memuat ID (PRD-/ORD-/TKT-/KB-) yang tidak pernah muncul di tool result
// atau pesan user pada sesi ini. Pelanggaran => jawaban diganti pesan aman.
// H4/H11: lihat guardAnswer (klaim aksi sukses tanpa tool sukses; jawaban kebijakan tanpa search_store_policy).
func AfterModel(ctx agent.CallbackContext, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
	if respErr != nil || resp == nil || resp.Partial || resp.Content == nil {
		return nil, nil
	}
	for _, p := range resp.Content.Parts {
		if p != nil && p.FunctionCall != nil {
			return nil, nil // masih memanggil tool: belum jawaban final
		}
	}
	text := contentText(resp.Content)
	policySeen := false
	if v, err := ctx.ReadonlyState().Get(StatePolicySeen); err == nil {
		policySeen = v == "1"
	}
	if msg := guardAnswer(text, turnTools(ctx.ReadonlyState().Get), policySeen); msg != "" {
		slog.Warn("guard H4/H11: jawaban diganti", "agent", ctx.AgentName())
		return &model.LLMResponse{Content: modelText(msg)}, nil
	}
	ids := idsIn(text)
	if len(ids) == 0 {
		return nil, nil
	}
	seen := seenSet(ctx.ReadonlyState().Get)
	for _, id := range ids {
		if !seen[id] {
			slog.Warn("guard H2: ID tidak terverifikasi pada jawaban", "id", id, "agent", ctx.AgentName())
			return &model.LLMResponse{Content: modelText(msgUnverified)}, nil
		}
	}
	return nil, nil
}

var (
	_ agent.BeforeAgentCallback   = BeforeAgent
	_ llmagent.BeforeToolCallback = BeforeTool
	_ llmagent.AfterToolCallback  = AfterTool
	_ llmagent.AfterModelCallback = AfterModel
)
