#!/usr/bin/env bash
# E2E: binary sungguhan catalog+order+ticket+mcp+gateway (tanpa mock). Jalankan dari root repo: bash scripts/e2e/run.sh (butuh go + python)
set -u
export INTERNAL_API_KEY=e2e-key JWT_SECRET=e2e-secret APP_ENV=production
export CATALOG_DB_PATH=./data/e2e/catalog.db ORDER_DB_PATH=./data/e2e/order.db TICKET_DB_PATH=./data/e2e/ticket.db AUTH_DB_PATH=./data/e2e/auth.db
rm -rf data/e2e; mkdir -p data/e2e bin
CGO_ENABLED=0 go build -o bin/ ./service-gateway ./service-catalog ./service-order ./service-ticket ./service-mcp || exit 1

PIDS=()
for s in catalog order ticket mcp gateway; do ./bin/service-$s.exe > data/e2e/$s.log 2>&1 & PIDS+=($!); done
trap 'kill ${PIDS[@]} 2>/dev/null' EXIT
for port in 4001 4002 4003 5000 8080; do
  for i in $(seq 1 50); do curl -s localhost:$port/health >/dev/null && break; sleep 0.2; done
done

G=localhost:8080
pass=0; fail=0
ok()  { pass=$((pass+1)); echo "  ok   - $1"; }
bad() { fail=$((fail+1)); echo "  FAIL - $1  (got: $2)"; }
check() { # nama, ekspektasi (substring), aktual
  if [[ "$3" == *"$2"* ]]; then ok "$1"; else bad "$1" "$3"; fi
}
jq_() { python -c "import sys,json; d=json.load(sys.stdin); print(eval(sys.argv[1]))" "$1"; }
login() { curl -s -X POST $G/auth/login -H 'content-type: application/json' -d "{\"email\":\"$1\",\"password\":\"123456\"}" | jq_ "d['data']['token']"; }

echo "== 1. auth =="
BUDI=$(login budi@toko.com); SITI=$(login siti@toko.com); ADMIN=$(login admin@toko.com); DEV=$(login dev@toko.com)
[[ -n "$BUDI" && -n "$ADMIN" ]] && ok "login 4 akun demo" || bad "login" "token kosong"
check "demo-users publik" '"USR-01"' "$(curl -s $G/auth/demo-users)"
check "tanpa token ke /api/orders => 401" 'UNAUTHENTICATED' "$(curl -s $G/api/orders)"

echo "== 2. katalog lewat gateway (GUEST) =="
check "GET produk powerbank<400rb" '"PRD-003"' "$(curl -s "$G/api/products?q=powerbank&max_price=400000")"
check "semantic-search tanpa API key => keyword_fallback" 'keyword_fallback' "$(curl -s "$G/api/products/semantic-search?q=mouse%20pergelangan%20tangan")"
check "produk nonaktif disembunyikan" 'NOT_FOUND' "$(curl -s $G/api/products/PRD-013)"
check "/internal diblokir gateway" 'NOT_FOUND' "$(curl -s -X POST $G/api/products/internal/stock/decrement)"
check "customer tidak boleh POST produk" 'FORBIDDEN' "$(curl -s -X POST $G/api/products -H "Authorization: Bearer $BUDI" -H 'content-type: application/json' -d '{"name":"abc"}')"

echo "== 3. alur belanja: gateway -> order -> catalog (stok) =="
STOCK0=$(curl -s $G/api/products/PRD-003 | jq_ "d['data']['stock']")
ORD=$(curl -s -X POST $G/api/orders -H "Authorization: Bearer $BUDI" -H 'content-type: application/json' \
  -d '{"items":[{"product_id":"PRD-003","quantity":2}],"payment_method":"EWALLET","shipping_address":"Jl. Merdeka No. 10, Jakarta"}')
OID=$(echo "$ORD" | jq_ "d['data']['id']")
check "order dibuat PENDING_PAYMENT" 'PENDING_PAYMENT' "$ORD"
check "total dari katalog (2 x 299000)" '598000' "$ORD"
STOCK1=$(curl -s $G/api/products/PRD-003 | jq_ "d['data']['stock']")
[[ $((STOCK0-2)) -eq $STOCK1 ]] && ok "stok katalog berkurang 2 ($STOCK0 -> $STOCK1)" || bad "stok" "$STOCK0 -> $STOCK1"
check "pay success => PAID" '"status":"PAID"' "$(curl -s -X POST $G/api/orders/$OID/pay -H "Authorization: Bearer $BUDI" -H 'content-type: application/json' -d '{"simulate":"success"}')"
ORD2=$(curl -s -X POST $G/api/orders -H "Authorization: Bearer $BUDI" -H 'content-type: application/json' \
  -d '{"items":[{"product_id":"PRD-004","quantity":1}],"payment_method":"EWALLET","shipping_address":"Jl. Merdeka No. 10, Jakarta"}')
OID2=$(echo "$ORD2" | jq_ "d['data']['id']")
S_A=$(curl -s $G/api/products/PRD-004 | jq_ "d['data']['stock']")
check "pay fail => PAYMENT_FAILED" 'PAYMENT_FAILED' "$(curl -s -X POST $G/api/orders/$OID2/pay -H "Authorization: Bearer $BUDI" -H 'content-type: application/json' -d '{"simulate":"fail"}')"
S_B=$(curl -s $G/api/products/PRD-004 | jq_ "d['data']['stock']")
[[ $((S_A+1)) -eq $S_B ]] && ok "stok dikembalikan setelah pembayaran gagal ($S_A -> $S_B)" || bad "restock" "$S_A -> $S_B"
check "stok kurang => 409 INSUFFICIENT_STOCK" 'INSUFFICIENT_STOCK' "$(curl -s -X POST $G/api/orders -H "Authorization: Bearer $BUDI" -H 'content-type: application/json' -d '{"items":[{"product_id":"PRD-012","quantity":50}],"payment_method":"EWALLET","shipping_address":"Jl. Merdeka No. 10, Jakarta"}')"

echo "== 4. isolasi data =="
check "Siti tidak bisa melihat ORD-003 milik Budi" 'NOT_FOUND' "$(curl -s $G/api/orders/ORD-003 -H "Authorization: Bearer $SITI")"
check "Budi melihat ORD-003 (gagal bayar seed)" 'Saldo e-wallet tidak mencukupi' "$(curl -s $G/api/orders/ORD-003 -H "Authorization: Bearer $BUDI")"
check "dev dapat membaca semua order" 'ORD-008' "$(curl -s "$G/api/orders?limit=100" -H "Authorization: Bearer $DEV")"
check "dev tidak boleh membayar" 'FORBIDDEN' "$(curl -s -X POST $G/api/orders/$OID/pay -H "Authorization: Bearer $DEV")"
check "header palsu x-user-role diabaikan" 'NOT_FOUND' "$(curl -s $G/api/orders/ORD-003 -H "Authorization: Bearer $SITI" -H 'x-user-role: ADMIN_SISTEM' -H 'x-user-id: USR-01')"

echo "== 5. tiket (gateway -> ticket -> order) =="
check "tiket untuk ORD-003 sudah ada => 409 CONFLICT" 'TKT-001' "$(curl -s -X POST $G/api/tickets -H "Authorization: Bearer $BUDI" -H 'content-type: application/json' -d '{"subject":"Gagal bayar lagi","description":"Pembayaran gagal terus menerus.","category":"PAYMENT","order_id":"ORD-003"}')"
check "tiket untuk order milik orang lain => 404" 'NOT_FOUND' "$(curl -s -X POST $G/api/tickets -H "Authorization: Bearer $BUDI" -H 'content-type: application/json' -d '{"subject":"Order orang","description":"Order bukan milik saya.","category":"PAYMENT","order_id":"ORD-005"}')"
check "tiket untuk order baru gagal bayar => 201" 'TKT-006' "$(curl -s -X POST $G/api/tickets -H "Authorization: Bearer $BUDI" -H 'content-type: application/json' -d "{\"subject\":\"Gagal bayar $OID2\",\"description\":\"Pembayaran pesanan gagal.\",\"category\":\"PAYMENT\",\"priority\":\"HIGH\",\"order_id\":\"$OID2\"}")"
check "admin memproses tiket" 'IN_PROGRESS' "$(curl -s -X PATCH $G/api/tickets/TKT-006/status -H "Authorization: Bearer $ADMIN" -H 'content-type: application/json' -d '{"status":"IN_PROGRESS"}')"
check "Siti tidak melihat tiket Budi" 'NOT_FOUND' "$(curl -s $G/api/tickets/TKT-006 -H "Authorization: Bearer $SITI")"

echo "== 6. knowledge base publik =="
check "GUEST mencari kebijakan (keyword_fallback tanpa API key)" 'Pembayaran Gagal dan Kedaluwarsa' "$(curl -s "$G/api/knowledge/search?q=pembayaran%20gagal%20kedaluwarsa")"
check "daftar dokumen KB" 'KB-008' "$(curl -s $G/api/knowledge/documents)"

echo "== 7. dev health =="
check "dev melihat status service" '"catalog":"up"' "$(curl -s $G/api/dev/health -H "Authorization: Bearer $DEV")"
check "agent (tidak dijalankan) => down" '"agent":"down"' "$(curl -s $G/api/dev/health -H "Authorization: Bearer $DEV")"

echo "== 8. MCP -> service Go (identitas diteruskan) =="
MCPOUT=$(go run ./scripts/e2e/mcpcheck 2>&1)
echo "$MCPOUT" | sed 's/^/    /'
check "MCP: semantic_search & check_stock tersedia untuk GUEST" 'PASS guest-tools' "$MCPOUT"
check "MCP: get_order milik sendiri berhasil, milik orang lain NOT_FOUND" 'PASS order-isolation' "$MCPOUT"
check "MCP: create_order lewat MCP memotong stok nyata" 'PASS create-order' "$MCPOUT"

echo
echo "HASIL E2E: $pass lulus, $fail gagal"
[[ $fail -eq 0 ]]
