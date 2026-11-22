import type { AdkEvent } from '~/types/api'

export interface ChatMessage {
  id: string
  from: 'user' | 'agent'
  text: string
}

const APP_NAME = 'toko_orchestrator'

/** Teks final agent = gabungan teks event non-partial terakhir yang ditulis agent (bukan user). */
export function extractFinalText(events: AdkEvent[]): string {
  for (let i = events.length - 1; i >= 0; i--) {
    const ev = events[i]
    if (!ev || ev.author === 'user' || ev.partial) continue
    const text = (ev.content?.parts ?? []).map((p) => p.text ?? '').join('').trim()
    if (text) return text
  }
  return ''
}

/**
 * Chat ke AI Agent lewat Gateway (/api/agent/**, protokol ADK camelCase).
 * Session dibuat per percakapan; Gateway/agent menyisipkan state identitas dari JWT.
 * Ganti akun (userId berubah) => percakapan di-reset otomatis.
 */
export function useChat() {
  const { $adk } = useApi()
  const auth = useAuth()

  const messages = useState<ChatMessage[]>('chat_messages', () => [])
  const sessionId = useState<string | null>('chat_session', () => null)
  const loading = useState<boolean>('chat_loading', () => false)
  const error = useState<string | null>('chat_error', () => null)
  const open = useState<boolean>('chat_open', () => false)
  const ownerId = useState<string>('chat_owner', () => auth.userId.value)

  function reset() {
    messages.value = []
    sessionId.value = null
    error.value = null
    ownerId.value = auth.userId.value
  }

  // Persistensi per userId di sessionStorage (try/catch: mode privat/blocked).
  const storeKey = (id: string) => `toko_chat_${id}`
  function restoreFromStorage(id: string) {
    try {
      const raw = sessionStorage.getItem(storeKey(id))
      if (!raw) return
      const saved = JSON.parse(raw) as { messages: ChatMessage[]; sessionId: string | null }
      messages.value = saved.messages ?? []
      sessionId.value = saved.sessionId ?? null
    } catch { /* abaikan */ }
  }
  if (import.meta.client && messages.value.length === 0) restoreFromStorage(auth.userId.value)
  watch([messages, sessionId], () => {
    try { sessionStorage.setItem(storeKey(auth.userId.value), JSON.stringify({ messages: messages.value, sessionId: sessionId.value })) } catch { /* abaikan */ }
  }, { deep: true })

  // userId berubah (RoleSwitcher / logout) => percakapan lama bukan milik identitas baru; muat milik identitas baru bila ada.
  watch(auth.userId, (id) => { if (id !== ownerId.value) { reset(); restoreFromStorage(id) } })

  async function ensureSession(userId: string): Promise<string> {
    if (sessionId.value) return sessionId.value
    const sid = `s-${crypto.randomUUID()}`
    await $adk(`/api/agent/apps/${APP_NAME}/users/${encodeURIComponent(userId)}/sessions/${sid}`, { method: 'POST', body: {} })
    sessionId.value = sid
    return sid
  }

  async function send(text: string) {
    const t = text.trim()
    if (!t || loading.value) return
    error.value = null
    messages.value.push({ id: crypto.randomUUID(), from: 'user', text: t })
    loading.value = true
    try {
      const userId = auth.userId.value
      const sid = await ensureSession(userId)
      const events = await $adk<AdkEvent[]>('/api/agent/run', {
        method: 'POST',
        body: { appName: APP_NAME, userId, sessionId: sid, newMessage: { role: 'user', parts: [{ text: t }] }, streaming: false },
      })
      const reply = extractFinalText(Array.isArray(events) ? events : [])
      messages.value.push({ id: crypto.randomUUID(), from: 'agent', text: reply || 'Maaf, saya tidak dapat menjawab saat ini.' })
    } catch (e) {
      const status = (e as { status?: number }).status
      if (status === 403) reset() // userId tidak cocok => sesi baru
      error.value = 'Maaf, asisten sedang bermasalah. Coba lagi.'
    } finally {
      loading.value = false
    }
  }

  return { messages, sessionId, loading, error, open, send, reset }
}
