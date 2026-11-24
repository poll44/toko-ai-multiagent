package agents // guard anti-halusinasi & penegakan aturan pada callback agent

import ( // package yang dipakai
	"encoding/json" // encode hasil tool untuk dipindai
	"log/slog"      // logging terstruktur
	"regexp"        // regular expression
	"slices"        // utilitas slice
	"strings"       // utilitas string

	"google.golang.org/adk/agent"          // tipe agent & callback context
	"google.golang.org/adk/agent/llmagent" // tipe callback llmagent
	"google.golang.org/adk/model"          // tipe respons LLM
	"google.golang.org/adk/tool"           // tipe tool
	"google.golang.org/genai"              // tipe konten GenAI

	"toko-ai-microservices/internal/shared/httputil" // role & validasi
)

// entityIDRe mencocokkan ID entitas (produk/pesanan/tiket/knowledge base), mis. PRD-003.
var entityIDRe = regexp.MustCompile(`\b(?:PRD|ORD|TKT|KB)-\d{3,}\b`)

// publicTools: satu-satunya tool yang boleh dipanggil GUEST (TRD 06 §6).
var publicTools = []string{"search_products", "semantic_search_products", "get_product", "list_categories", "check_stock", "search_store_policy"}

// writeTools wajib confirmed_by_user=true (konfirmasi eksplisit 2 langkah, H8).
var writeTools = []string{"create_order", "pay_order", "cancel_order", "create_ticket"}

// Pesan pengganti yang aman bagi pengguna.
const (
	msgInvalidSession = "Maaf, sesi percakapan ini tidak valid. Silakan mulai percakapan baru."                                            // sesi tidak sah
	msgUnverified     = "Maaf, saya tidak dapat memverifikasi data tersebut. Mohon ulangi pertanyaan Anda atau sebutkan nomor yang benar." // ID tak terverifikasi
)

// modelText membuat konten bertipe jawaban model dari teks biasa.
func modelText(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }

// idsIn mengembalikan semua ID entitas yang ada di dalam teks s.
func idsIn(s string) []string { return entityIDRe.FindAllString(s, -1) }

// contentText menggabungkan seluruh teks dari semua part sebuah konten.
func contentText(c *genai.Content) string {
	if c == nil { // tidak ada konten
		return ""
	}
	var b strings.Builder       // penampung hasil
	for _, p := range c.Parts { // telusuri tiap part
		if p != nil {
			b.WriteString(p.Text) // tambah teks part
			b.WriteByte('\n')     // pisahkan dengan baris baru
		}
	}
	return b.String()
}

// seenIDs membaca himpunan ID yang pernah terlihat di sesi (state string "ID1,ID2").
func seenSet(get func(string) (any, error)) map[string]bool {
	set := map[string]bool{}                     // himpunan hasil
	if v, err := get(StateSeenIDs); err == nil { // baca state seen_ids
		if s, ok := v.(string); ok { // harus string
			for id := range strings.SplitSeq(s, ",") { // pecah per koma
				if id != "" {
					set[id] = true
				}
			}
		}
	}
	return set
}

// addSeen menambahkan ids ke himpunan seen_ids di session state.
func addSeen(ctx agent.CallbackContext, ids []string) {
	if len(ids) == 0 { // tidak ada yang ditambah
		return
	}
	set := seenSet(ctx.ReadonlyState().Get) // himpunan saat ini
	for _, id := range ids {                // gabungkan ID baru
		set[id] = true
	}
	list := make([]string, 0, len(set)) // ubah ke slice
	for id := range set {
		list = append(list, id)
	}
	slices.Sort(list)                                                              // urutkan agar deterministik
	if err := ctx.State().Set(StateSeenIDs, strings.Join(list, ",")); err != nil { // simpan kembali
		slog.Warn("gagal menyimpan seen_ids", "err", err)
	}
}

// BeforeAgent: validasi identitas session & catat ID yang disebut user (agar tidak dianggap halusinasi).
// state.user_id harus sama dengan userId invocation — mencegah sesi milik user lain dipakai (TRD 06 §2 aturan 2).
func BeforeAgent(ctx agent.CallbackContext) (*genai.Content, error) {
	if v, err := ctx.ReadonlyState().Get(StateUserID); err == nil { // baca user_id di state
		if s, _ := v.(string); s != "" && s != ctx.UserID() { // beda dengan userId invocation
			slog.Warn("identitas session tidak cocok dengan userId", "state_user", s, "user", ctx.UserID())
			return modelText(msgInvalidSession), nil // hentikan agent, balas pesan aman
		}
	}
	if v, err := ctx.ReadonlyState().Get(StateUserRole); err == nil { // baca role di state
		if s, _ := v.(string); s != "" && !httputil.ValidRole(s) { // role tidak dikenal
			return modelText(msgInvalidSession), nil
		}
	}
	startTurn(ctx)                                      // reset pelacakan bila giliran baru
	addSeen(ctx, idsIn(contentText(ctx.UserContent()))) // ID yang disebut user dianggap sah
	return nil, nil                                     // lanjutkan eksekusi agent
}

// blocked membuat hasil tool tiruan berformat error standar (untuk memblokir pemanggilan tool).
func blocked(code, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": msg, "details": nil}}
}

// BeforeTool: GUEST hanya tool publik; hapus argumen identitas liar; tool tulis wajib confirmed_by_user=true.
func BeforeTool(ctx agent.ToolContext, t tool.Tool, args map[string]any) (map[string]any, error) {
	_, role := IdentityFromState(ctx.ReadonlyState())                      // role pemanggil
	name := t.Name()                                                       // nama tool yang akan dipanggil
	if role == httputil.RoleGuest && !slices.Contains(publicTools, name) { // tamu memanggil tool non-publik
		return blocked("FORBIDDEN", "Silakan login terlebih dahulu untuk menggunakan fitur ini."), nil
	}
	for _, k := range []string{"user_id", "userId", "role", "user_role"} { // argumen identitas terlarang
		delete(args, k) // LLM tidak boleh mengisi identitas
	}
	if slices.Contains(writeTools, name) { // tool yang mengubah data
		if ok, _ := args["confirmed_by_user"].(bool); !ok { // belum ada konfirmasi
			return blocked("CONFIRMATION_REQUIRED",
				"Aksi ini membutuhkan konfirmasi eksplisit dari pelanggan. Tampilkan ringkasan dan tanyakan 'Lanjutkan?' dulu."), nil
		}
	}
	return nil, nil // izinkan pemanggilan tool
}

// AfterTool: catat ID entitas dari hasil tool ke seen_ids (H2), tool yang sukses pada giliran ini (H4),
// dan apakah kebijakan resmi pernah ditemukan (H11).
func AfterTool(ctx agent.ToolContext, t tool.Tool, _ map[string]any, result map[string]any, toolErr error) (map[string]any, error) {
	if result == nil { // tidak ada hasil
		return nil, nil
	}
	b, err := json.Marshal(result) // serialisasi hasil untuk dipindai
	if err != nil {
		return nil, nil
	}
	addSeen(ctx, idsIn(string(b))) // semua ID di hasil tool dianggap sah
	if toolErr != nil || result["error"] != nil {
		return nil, nil // gagal: bukan bukti aksi sukses
	}
	st := ctx.State()                           // state tulis
	tools := turnTools(ctx.ReadonlyState().Get) // tool sukses giliran ini
	tools[t.Name()] = true                      // tandai tool ini sukses
	if e := st.Set(StateTurnTools, joinSet(tools)); e != nil {
		slog.Warn("gagal menyimpan turn_tools", "err", e)
	}
	if t.Name() == "search_store_policy" && strings.Contains(string(b), `"chunk_id"`) { // kebijakan benar-benar ditemukan
		_ = st.Set(StatePolicySeen, "1")
	}
	return nil, nil // tidak mengubah hasil tool
}

// joinSet menggabungkan himpunan nama menjadi string terurut "a,b,c".
func joinSet(set map[string]bool) string {
	list := make([]string, 0, len(set)) // ubah ke slice
	for k := range set {
		list = append(list, k)
	}
	slices.Sort(list)              // urutkan
	return strings.Join(list, ",") // gabung dengan koma
}

// turnTools membaca himpunan nama tool sukses pada giliran ini dari state.
func turnTools(get func(string) (any, error)) map[string]bool {
	set := map[string]bool{}                       // himpunan hasil
	if v, err := get(StateTurnTools); err == nil { // baca state
		if s, ok := v.(string); ok {
			for n := range strings.SplitSeq(s, ",") { // pecah per koma
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
	if v, err := ctx.ReadonlyState().Get(StateTurnInv); err == nil { // invocation tersimpan
		if s, _ := v.(string); s == ctx.InvocationID() { // masih giliran yang sama
			return
		}
	}
	_ = ctx.State().Set(StateTurnInv, ctx.InvocationID()) // simpan ID giliran baru
	_ = ctx.State().Set(StateTurnTools, "")               // kosongkan daftar tool sukses
}

// Pesan pengganti untuk guard H4/H11.
const (
	msgUnconfirmedAction = "Maaf, saya belum dapat memastikan aksi tersebut berhasil. Silakan periksa menu Pesanan/Tiket, atau minta saya mengecek statusnya." // klaim sukses tanpa bukti
	msgNoPolicy          = "Maaf, saat ini belum ada informasi resmi mengenai hal tersebut. Anda dapat membuat tiket agar tim kami membantu."                  // kebijakan tanpa sumber
)

// H4: klaim aksi sukses orang-pertama ("berhasil dibuat", "sudah saya batalkan") harus didukung tool sukses pada giliran ini.
var actionClaims = []struct {
	re    *regexp.Regexp // pola klaim di teks jawaban
	tools []string       // tool yang harus sukses untuk mendukung klaim
}{
	{regexp.MustCompile(`(?i)\bberhasil\s+(?:saya\s+)?(?:dibuat|dibuatkan|membuat)\b|\b(?:telah|sudah)\s+saya\s+(?:buat|buatkan)\b`), []string{"create_order", "create_ticket"}}, // klaim membuat
	{regexp.MustCompile(`(?i)\bberhasil\s+(?:saya\s+)?dibatalkan\b|\b(?:telah|sudah)\s+saya\s+batalkan\b`), []string{"cancel_order"}},                                            // klaim membatalkan
	{regexp.MustCompile(`(?i)\bberhasil\s+(?:saya\s+)?(?:dibayar|dibayarkan)\b|\b(?:telah|sudah)\s+saya\s+bayar(?:kan)?\b`), []string{"pay_order"}},                              // klaim membayar
}

// H11: jawaban bernada kebijakan (kata kunci + angka berunit) wajib berasal dari search_store_policy.
var (
	policyKeywordRe = regexp.MustCompile(`(?i)\b(retur|refund|pengembalian dana|estimasi pengiriman|garansi)\b`) // kata kunci kebijakan
	policyNumberRe  = regexp.MustCompile(`(?i)\b\d+\s*(?:hari|jam|bulan|tahun|%|persen)`)                        // angka berunit
	productTools    = []string{"get_product", "search_products", "semantic_search_products", "check_stock"}      // tool data produk
)

// guardAnswer mengembalikan pesan pengganti bila jawaban melanggar H4/H11, atau "" bila aman.
func guardAnswer(text string, okTools map[string]bool, policySeen bool) string {
	for _, c := range actionClaims { // periksa tiap jenis klaim aksi
		if !c.re.MatchString(text) { // klaim tidak muncul
			continue
		}
		if !slices.ContainsFunc(c.tools, func(n string) bool { return okTools[n] }) { // tak ada tool sukses pendukung
			return msgUnconfirmedAction
		}
	}
	if !policySeen && !okTools["search_store_policy"] && policyNumberRe.MatchString(text) { // ada angka tanpa sumber kebijakan
		kws := policyKeywordRe.FindAllString(text, -1) // kata kunci kebijakan yang muncul
		if len(kws) > 0 {
			onlyWarranty := true // apakah hanya membahas garansi
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
	return "" // jawaban aman
}

// AfterModel (H2): jawaban final tidak boleh memuat ID (PRD-/ORD-/TKT-/KB-) yang tidak pernah muncul di tool result
// atau pesan user pada sesi ini. Pelanggaran => jawaban diganti pesan aman.
// H4/H11: lihat guardAnswer (klaim aksi sukses tanpa tool sukses; jawaban kebijakan tanpa search_store_policy).
func AfterModel(ctx agent.CallbackContext, resp *model.LLMResponse, respErr error) (*model.LLMResponse, error) {
	if respErr != nil || resp == nil || resp.Partial || resp.Content == nil { // bukan jawaban lengkap
		return nil, nil
	}
	for _, p := range resp.Content.Parts { // cek apakah ada pemanggilan tool
		if p != nil && p.FunctionCall != nil {
			return nil, nil // masih memanggil tool: belum jawaban final
		}
	}
	text := contentText(resp.Content) // teks jawaban
	policySeen := false               // apakah kebijakan pernah ditemukan
	if v, err := ctx.ReadonlyState().Get(StatePolicySeen); err == nil {
		policySeen = v == "1"
	}
	if msg := guardAnswer(text, turnTools(ctx.ReadonlyState().Get), policySeen); msg != "" { // pelanggaran H4/H11
		slog.Warn("guard H4/H11: jawaban diganti", "agent", ctx.AgentName())
		return &model.LLMResponse{Content: modelText(msg)}, nil // ganti jawaban
	}
	ids := idsIn(text) // ID entitas yang disebut di jawaban
	if len(ids) == 0 {
		return nil, nil
	}
	seen := seenSet(ctx.ReadonlyState().Get) // ID yang pernah terlihat
	for _, id := range ids {
		if !seen[id] { // ID tidak pernah muncul: kemungkinan halusinasi
			slog.Warn("guard H2: ID tidak terverifikasi pada jawaban", "id", id, "agent", ctx.AgentName())
			return &model.LLMResponse{Content: modelText(msgUnverified)}, nil
		}
	}
	return nil, nil // jawaban lolos
}

// Pastikan fungsi callback sesuai tipe yang diharapkan ADK (cek saat kompilasi).
var (
	_ agent.BeforeAgentCallback   = BeforeAgent
	_ llmagent.BeforeToolCallback = BeforeTool
	_ llmagent.AfterToolCallback  = AfterTool
	_ llmagent.AfterModelCallback = AfterModel
)
