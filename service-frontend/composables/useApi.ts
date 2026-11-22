import type { ApiMeta } from '~/types/api'

export class ApiError extends Error {
  status: number
  code: string
  details: unknown
  request_id: string
  constructor(status: number, code: string, message: string, details: unknown = null, request_id = '') {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
    this.request_id = request_id
  }
}

export interface ApiOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  query?: Record<string, string | number | boolean | undefined | null>
  signal?: AbortSignal
}

/** Token JWT di localStorage (try/catch: mode privat/blocked) dengan fallback in-memory. */
const TOKEN_KEY = 'toko_token'
let memoryToken: string | null = null

export const tokenStore = {
  get(): string | null {
    try { return localStorage.getItem(TOKEN_KEY) ?? memoryToken } catch { return memoryToken }
  },
  set(t: string) {
    memoryToken = t
    try { localStorage.setItem(TOKEN_KEY, t) } catch { /* abaikan */ }
  },
  clear() {
    memoryToken = null
    try { localStorage.removeItem(TOKEN_KEY) } catch { /* abaikan */ }
  },
}

/**
 * Klien API ke Gateway (http://localhost:8080) — langsung dari browser.
 * - Menyuntik Authorization: Bearer <token> bila ada. TIDAK pernah mengirim header x-user-id, x-user-role, atau x-internal-api-key.
 * - $api mengembalikan isi `data`; $apiList mengembalikan {data, meta}.
 * - Error standar {error:{code,message,details,request_id}} => ApiError.
 */
export function useApi() {
  const base = useRuntimeConfig().public.apiBase as string

  async function raw(path: string, opts: ApiOptions = {}): Promise<unknown> {
    const headers: Record<string, string> = { 'x-request-id': crypto.randomUUID() }
    const token = tokenStore.get()
    if (token) headers.Authorization = `Bearer ${token}`
    if (opts.body !== undefined) headers['Content-Type'] = 'application/json'

    let res: Response
    try {
      const url = new URL(path, base)
      for (const [k, v] of Object.entries(opts.query ?? {})) {
        if (v !== undefined && v !== null && v !== '') url.searchParams.set(k, String(v))
      }
      res = await fetch(url, {
        method: opts.method ?? 'GET',
        headers,
        body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
        signal: opts.signal,
      })
    } catch (e) {
      if ((e as Error).name === 'AbortError') throw e
      throw new ApiError(0, 'NETWORK_ERROR', 'Tidak dapat menghubungi server. Periksa koneksi Anda.')
    }

    if (res.status === 204) return undefined
    const text = await res.text()
    let json: any = null
    try { json = text ? JSON.parse(text) : null } catch { /* bukan JSON */ }

    if (!res.ok) {
      const e = json?.error
      const err = new ApiError(res.status, e?.code ?? 'UNKNOWN_ERROR', e?.message ?? `Permintaan gagal (${res.status}).`, e?.details ?? null, e?.request_id ?? '')
      if (res.status === 401 && err.code === 'UNAUTHENTICATED' && token) {
        tokenStore.clear()
        useState('auth_user').value = null
        await navigateTo('/')
      }
      throw err
    }
    return json
  }

  async function $api<T>(path: string, opts?: ApiOptions): Promise<T> {
    const body = (await raw(path, opts)) as { data: T } | undefined
    return body?.data as T
  }

  async function $apiList<T>(path: string, opts?: ApiOptions): Promise<{ data: T[]; meta: ApiMeta }> {
    const body = (await raw(path, opts)) as { data: T[]; meta: ApiMeta }
    return { data: body?.data ?? [], meta: body?.meta ?? {} }
  }

  /** Jalur agent: protokol ADK (camelCase), tanpa envelope {data}. */
  async function $adk<T>(path: string, opts?: ApiOptions): Promise<T> {
    return (await raw(path, opts)) as T
  }

  return { $api, $apiList, $adk }
}
