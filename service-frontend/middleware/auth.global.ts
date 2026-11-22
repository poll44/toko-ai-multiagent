// Guard rute (UX saja — keamanan sebenarnya di backend). PRD 07 §2.
import type { Role } from '~/types/api'

// Prefiks rute proteksi => role yang boleh. Rute lain publik.
const RULES: { prefix: string; roles: Role[] }[] = [
  { prefix: '/admin', roles: ['ADMIN_SISTEM'] },
  { prefix: '/dev', roles: ['ADMIN_DEV'] },
  { prefix: '/orders', roles: ['CUSTOMER', 'ADMIN_SISTEM', 'ADMIN_DEV'] },
  { prefix: '/tickets', roles: ['CUSTOMER', 'ADMIN_SISTEM', 'ADMIN_DEV'] },
  { prefix: '/checkout', roles: ['CUSTOMER'] },
]

export default defineNuxtRouteMiddleware((to) => {
  const auth = useAuth()

  // Daftar milik sendiri: admin diarahkan ke dashboard masing-masing (detail /orders/[id] & /tickets/[id] tetap boleh dibaca).
  if (to.path === '/orders' || to.path === '/tickets') {
    if (auth.role.value === 'ADMIN_SISTEM') return navigateTo(`/admin${to.path}`)
    if (auth.role.value === 'ADMIN_DEV') return navigateTo('/dev')
  }
  if (to.path === '/cart' && auth.isAuthenticated.value && auth.role.value !== 'CUSTOMER') return navigateTo('/')
  if (to.path === '/tickets/new' && auth.isAuthenticated.value && auth.role.value !== 'CUSTOMER') return navigateTo('/')

  const rule = RULES.find((r) => to.path === r.prefix || to.path.startsWith(`${r.prefix}/`))
  if (!rule) return
  if (!auth.isAuthenticated.value) {
    // Tanpa login: kembali ke beranda; pengguna memilih akun lewat RoleSwitcher (redirect disimpan).
    useToast().info('Pilih akun demo di pojok kanan atas untuk melanjutkan.')
    return navigateTo({ path: '/', query: { redirect: to.fullPath } })
  }
  if (!rule.roles.includes(auth.role.value)) {
    useToast().error('Akses ditolak.')
    return navigateTo('/')
  }
})
