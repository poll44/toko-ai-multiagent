export interface Toast { id: number; kind: 'success' | 'error' | 'info'; text: string }
let seq = 0

export function useToast() {
  const toasts = useState<Toast[]>('toasts', () => [])
  function push(kind: Toast['kind'], text: string) {
    const id = ++seq
    toasts.value.push({ id, kind, text })
    setTimeout(() => { toasts.value = toasts.value.filter((t) => t.id !== id) }, 4500)
  }
  return {
    toasts,
    success: (t: string) => push('success', t),
    error: (t: string) => push('error', t),
    info: (t: string) => push('info', t),
  }
}

/** Pesan error API + request_id kecil untuk debugging. */
export function errText(e: unknown): string {
  const err = e as { message?: string; request_id?: string }
  return (err.message ?? 'Terjadi kesalahan.') + (err.request_id ? ` (ref: ${err.request_id.slice(0, 8)})` : '')
}
