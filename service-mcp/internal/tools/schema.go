package tools

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// Helper pembangun JSON Schema (mirror Zod `.strict()` di PRD 05): objek selalu additionalProperties=false,
// sehingga argumen tak dikenal (mis. user_id) DITOLAK sebelum handler dijalankan.
type m = map[string]any

func strSchema(minLen, maxLen int, pattern string) m {
	s := m{"type": "string"}
	if minLen > 0 {
		s["minLength"] = minLen
	}
	if maxLen > 0 {
		s["maxLength"] = maxLen
	}
	if pattern != "" {
		s["pattern"] = pattern
	}
	return s
}

func intSchema(minV, maxV int) m { return m{"type": "integer", "minimum": minV, "maximum": maxV} }
func intDefault(minV, maxV, def int) m {
	s := intSchema(minV, maxV)
	s["default"] = def
	return s
}
func enumSchema(vals ...string) m { return m{"type": "string", "enum": vals} }
func enumDefault(def string, vals ...string) m {
	s := enumSchema(vals...)
	s["default"] = def
	return s
}
func boolSchema() m { return m{"type": "boolean"} }
func constTrue() m  { return m{"type": "boolean", "const": true} }
func arraySchema(item m, minItems, maxItems int) m {
	return m{"type": "array", "items": item, "minItems": minItems, "maxItems": maxItems}
}

// object membangun skema objek strict. required: nama properti wajib.
func object(props m, required ...string) m {
	o := m{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

// compile mengubah skema (map) menjadi *jsonschema.Schema; panic saat startup bila tidak valid.
func compile(s m) *jsonschema.Schema {
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("schema tidak dapat di-marshal: %v", err))
	}
	var out jsonschema.Schema
	if err := json.Unmarshal(b, &out); err != nil {
		panic(fmt.Sprintf("schema tidak valid: %v", err))
	}
	return &out
}

const (
	productIDPattern = `^PRD-\d{3,}$`
	orderIDPattern   = `^ORD-\d{3,}$`
	ticketIDPattern  = `^TKT-\d{3,}$`
)

var (
	categories    = []string{"audio", "aksesoris", "wearable", "komputer", "penyimpanan"}
	orderStatuses = []string{"PENDING_PAYMENT", "PAID", "PROCESSING", "SHIPPED", "DELIVERED", "CANCELLED", "PAYMENT_FAILED", "PAYMENT_TIMEOUT"}
	ticketStates  = []string{"OPEN", "IN_PROGRESS", "RESOLVED", "CLOSED"}
	kbCategories  = []string{"RETURN", "WARRANTY", "SHIPPING", "PAYMENT", "ORDER", "COMPLAINT", "GENERAL"}
)
