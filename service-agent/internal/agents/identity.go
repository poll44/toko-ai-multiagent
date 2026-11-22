// Package agents: Orchestrator + 2 sub-agent (PRD 06), toolset MCP per-identitas, dan guard.
package agents

import (
	"toko-ai-microservices/internal/shared/httputil"

	"google.golang.org/adk/session"
)

// Kunci session state yang diisi di sisi server (Gateway + middleware agent). Satu-satunya sumber identitas.
const (
	StateUserID   = "user_id"
	StateUserRole = "user_role"
	StateSeenIDs  = "seen_ids" // ID entitas (PRD/ORD/TKT/KB) yang sudah muncul di sesi — untuk guard anti-halusinasi H2

	// Pelacakan per giliran (invocation) untuk guard H4 & H11.
	StateTurnInv    = "turn_inv"    // invocation ID giliran yang sedang berjalan; berubah => reset turn_tools
	StateTurnTools  = "turn_tools"  // nama tool yang SUKSES pada giliran ini ("a,b")
	StatePolicySeen = "policy_seen" // "1" bila search_store_policy pernah mengembalikan isi kebijakan di sesi ini
)

// IdentityFromState membaca identitas dari session state. Tidak ada/invalid => GUEST (PRD 06 §2 aturan 4).
func IdentityFromState(st session.ReadonlyState) (userID, role string) {
	userID, role = httputil.RoleGuest, httputil.RoleGuest
	if st == nil {
		return
	}
	if v, err := st.Get(StateUserID); err == nil {
		if s, ok := v.(string); ok && s != "" {
			userID = s
		}
	}
	if v, err := st.Get(StateUserRole); err == nil {
		if s, ok := v.(string); ok && httputil.ValidRole(s) {
			role = s
		}
	}
	return
}
