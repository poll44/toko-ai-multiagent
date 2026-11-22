import type { AuthUser, LoginResult, Role } from '~/types/api'

// Password demo (khusus demo; PRD 07 §3.1) — hanya dipakai di composable ini.
const DEMO_PASSWORD = '123456'

/**
 * Autentikasi tanpa form login: RoleSwitcher memanggil loginAs(email) dengan password demo.
 * JWT di localStorage['toko_token']; state reaktif dibagikan lewat useState (SPA, ssr:false).
 */
export function useAuth() {
  const { $api } = useApi()

  const user = useState<AuthUser | null>('auth_user', () => null)
  const demoUsers = useState<AuthUser[]>('auth_demo_users', () => [])
  const ready = useState<boolean>('auth_ready', () => false)

  const role = computed<Role>(() => user.value?.role ?? 'GUEST')
  const userId = computed<string>(() => user.value?.id ?? 'GUEST')
  const isAuthenticated = computed(() => user.value !== null)

  async function loadDemoUsers() {
    demoUsers.value = await $api<AuthUser[]>('/auth/demo-users')
  }

  async function loginAs(email: string) {
    const res = await $api<LoginResult>('/auth/login', { method: 'POST', body: { email, password: DEMO_PASSWORD } })
    tokenStore.set(res.token)
    user.value = res.user
  }

  function logout() {
    tokenStore.clear()
    user.value = null
  }

  /** Pulihkan sesi dari token tersimpan (dipanggil sekali saat start oleh plugin). */
  async function restore() {
    if (!tokenStore.get()) { ready.value = true; return }
    try {
      user.value = await $api<AuthUser>('/auth/me')
    } catch {
      logout()
    } finally {
      ready.value = true
    }
  }

  function hasRole(...roles: Role[]) {
    return roles.includes(role.value)
  }

  return { user, demoUsers, ready, role, userId, isAuthenticated, loadDemoUsers, loginAs, logout, restore, hasRole }
}
