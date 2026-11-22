// Keranjang di localStorage['toko_cart'] (PRD 07 §2.1 /cart). Harga final selalu dihitung server saat checkout.
export interface CartItem { product_id: string; name: string; price: number; stock: number; quantity: number }
const KEY = 'toko_cart'

export function useCart() {
  const items = useState<CartItem[]>('cart_items', () => [])
  const loaded = useState<boolean>('cart_loaded', () => false)

  function persist() { try { localStorage.setItem(KEY, JSON.stringify(items.value)) } catch { /* abaikan */ } }
  if (import.meta.client && !loaded.value) {
    loaded.value = true
    try { items.value = JSON.parse(localStorage.getItem(KEY) ?? '[]') } catch { items.value = [] }
  }

  const count = computed(() => items.value.reduce((s, i) => s + i.quantity, 0))
  const total = computed(() => items.value.reduce((s, i) => s + i.quantity * i.price, 0))

  function add(p: { id: string; name: string; price: number; stock: number }, qty = 1) {
    const cur = items.value.find((i) => i.product_id === p.id)
    if (cur) { cur.quantity = Math.min(cur.quantity + qty, Math.max(p.stock, 1), 99); cur.stock = p.stock; cur.price = p.price }
    else items.value.push({ product_id: p.id, name: p.name, price: p.price, stock: p.stock, quantity: Math.min(qty, 99) })
    persist()
  }
  function setQty(id: string, qty: number) {
    const cur = items.value.find((i) => i.product_id === id)
    if (!cur) return
    cur.quantity = Math.max(1, Math.min(Math.floor(qty) || 1, 99))
    persist()
  }
  function remove(id: string) { items.value = items.value.filter((i) => i.product_id !== id); persist() }
  function clear() { items.value = []; persist() }

  return { items, count, total, add, setQty, remove, clear }
}
