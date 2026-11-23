// Cermin kontrak backend Go (snake_case, null eksplisit, envelope {data}/{error}). Master TRD §7, Frontend TRD §6.
// Perubahan skema backend WAJIB disertai pembaruan file ini.

export type Role = 'GUEST' | 'CUSTOMER' | 'ADMIN_SISTEM' | 'ADMIN_DEV'

export interface AuthUser {
  id: string
  email: string
  name: string
  role: Exclude<Role, 'GUEST'>
}

export interface LoginResult {
  token: string
  token_type: 'Bearer'
  expires_in: number
  user: AuthUser
}

export interface ApiMeta {
  total?: number
  page?: number
  limit?: number
  mode?: 'semantic' | 'keyword_fallback'
  query?: string
  min_score?: number
}

export type StockStatus = 'IN_STOCK' | 'LOW_STOCK' | 'OUT_OF_STOCK'

export interface Product {
  id: string
  name: string
  description: string
  specifications: string
  category: string
  price: number
  stock: number
  stock_status: StockStatus
  image_url: string | null
  is_active: boolean
  created_at: string
  updated_at: string
  /** hanya pada semantic-search; null pada keyword_fallback */
  score?: number | null
}

export type KnowledgeCategory = 'RETURN' | 'WARRANTY' | 'SHIPPING' | 'PAYMENT' | 'ORDER' | 'COMPLAINT' | 'GENERAL'

export interface KnowledgeChunk {
  chunk_id: string
  document_id: string
  title: string
  category: KnowledgeCategory
  content: string
  score: number | null
}

export interface KnowledgeDocument {
  id: string
  title: string
  category: KnowledgeCategory
  content?: string
  is_active: boolean
  chunk_count: number
  created_at: string
  updated_at: string
}

export interface OrderItem {
  product_id: string
  product_name: string
  unit_price: number
  quantity: number
  subtotal: number
}

export type OrderStatus =
  | 'PENDING_PAYMENT' | 'PAID' | 'PROCESSING' | 'SHIPPED'
  | 'DELIVERED' | 'CANCELLED' | 'PAYMENT_FAILED' | 'PAYMENT_TIMEOUT'

export interface Order {
  id: string
  user_id: string
  status: OrderStatus
  payment_status: 'UNPAID' | 'PAID' | 'FAILED' | 'EXPIRED' | 'REFUNDED'
  payment_method: 'EWALLET' | 'BANK_TRANSFER' | 'CREDIT_CARD'
  total_amount: number
  shipping_address: string
  tracking_number: string | null
  failure_reason: string | null
  payment_expires_at: string | null
  paid_at: string | null
  items: OrderItem[]
  history?: { from_status: string | null; to_status: string; note: string | null; actor_id: string; created_at: string }[]
  created_at: string
  updated_at: string
}

export interface Ticket {
  id: string
  user_id: string
  order_id: string | null
  subject: string
  description: string
  category: 'PAYMENT' | 'SHIPPING' | 'PRODUCT' | 'TECHNICAL' | 'OTHER'
  priority: 'LOW' | 'MEDIUM' | 'HIGH'
  status: 'OPEN' | 'IN_PROGRESS' | 'RESOLVED' | 'CLOSED'
  assigned_to: string | null
  resolution_note: string | null
  created_at: string
  updated_at: string
  resolved_at: string | null
  closed_at: string | null
  messages?: { id: number; author_id: string; author_role: string; message: string; created_at: string }[]
}

/** Protokol ADK (camelCase) — satu-satunya payload non-snake_case, hanya di jalur /api/agent/**. */
export interface AdkPart { text?: string; functionCall?: unknown; functionResponse?: unknown }
export interface AdkEvent {
  author?: string
  content?: { role?: string; parts?: AdkPart[] }
  partial?: boolean
  turnComplete?: boolean
}
