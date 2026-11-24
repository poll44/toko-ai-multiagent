package tools // pembangun JSON Schema untuk MCP tools

import ( // package yang dipakai
	"encoding/json" // marshal/unmarshal skema
	"fmt"           // format pesan panic

	"github.com/google/jsonschema-go/jsonschema" // tipe JSON Schema
)

// Helper pembangun JSON Schema (mirror Zod `.strict()` di PRD 05): objek selalu additionalProperties=false,
// sehingga argumen tak dikenal (mis. user_id) DITOLAK sebelum handler dijalankan.
type m = map[string]any

// strSchema membuat skema string dengan batas panjang & pola regex opsional (0/"" = tanpa batas).
func strSchema(minLen, maxLen int, pattern string) m {
	s := m{"type": "string"} // skema dasar
	if minLen > 0 {          // panjang minimum
		s["minLength"] = minLen
	}
	if maxLen > 0 { // panjang maksimum
		s["maxLength"] = maxLen
	}
	if pattern != "" { // pola regex
		s["pattern"] = pattern
	}
	return s
}

// intSchema membuat skema integer dengan rentang minV..maxV.
func intSchema(minV, maxV int) m { return m{"type": "integer", "minimum": minV, "maximum": maxV} }

// intDefault seperti intSchema tetapi dengan nilai default.
func intDefault(minV, maxV, def int) m {
	s := intSchema(minV, maxV) // skema dasar
	s["default"] = def         // nilai default
	return s
}

// enumSchema membuat skema string yang hanya boleh bernilai salah satu dari vals.
func enumSchema(vals ...string) m { return m{"type": "string", "enum": vals} }

// enumDefault seperti enumSchema tetapi dengan nilai default.
func enumDefault(def string, vals ...string) m {
	s := enumSchema(vals...) // skema dasar
	s["default"] = def       // nilai default
	return s
}

// boolSchema membuat skema boolean.
func boolSchema() m { return m{"type": "boolean"} }

// constTrue membuat skema boolean yang HARUS bernilai true (dipakai untuk confirmed_by_user).
func constTrue() m { return m{"type": "boolean", "const": true} }

// arraySchema membuat skema array dengan skema item serta batas jumlah item.
func arraySchema(item m, minItems, maxItems int) m {
	return m{"type": "array", "items": item, "minItems": minItems, "maxItems": maxItems}
}

// object membangun skema objek strict. required: nama properti wajib.
func object(props m, required ...string) m {
	o := m{"type": "object", "properties": props, "additionalProperties": false} // tolak properti tak dikenal
	if len(required) > 0 {                                                       // ada properti wajib
		o["required"] = required
	}
	return o
}

// compile mengubah skema (map) menjadi *jsonschema.Schema; panic saat startup bila tidak valid.
func compile(s m) *jsonschema.Schema {
	b, err := json.Marshal(s) // map -> JSON
	if err != nil {
		panic(fmt.Sprintf("schema tidak dapat di-marshal: %v", err))
	}
	var out jsonschema.Schema
	if err := json.Unmarshal(b, &out); err != nil { // JSON -> Schema
		panic(fmt.Sprintf("schema tidak valid: %v", err))
	}
	return &out
}

// Pola regex ID entitas.
const (
	productIDPattern = `^PRD-\d{3,}$` // ID produk
	orderIDPattern   = `^ORD-\d{3,}$` // ID pesanan
	ticketIDPattern  = `^TKT-\d{3,}$` // ID tiket
)

// Daftar nilai enum yang dipakai pada skema tool.
var (
	categories    = []string{"audio", "aksesoris", "wearable", "komputer", "penyimpanan"}                                                       // kategori produk
	orderStatuses = []string{"PENDING_PAYMENT", "PAID", "PROCESSING", "SHIPPED", "DELIVERED", "CANCELLED", "PAYMENT_FAILED", "PAYMENT_TIMEOUT"} // status pesanan
	ticketStates  = []string{"OPEN", "IN_PROGRESS", "RESOLVED", "CLOSED"}                                                                       // status tiket
	kbCategories  = []string{"RETURN", "WARRANTY", "SHIPPING", "PAYMENT", "ORDER", "COMPLAINT", "GENERAL"}                                      // kategori knowledge base
)
