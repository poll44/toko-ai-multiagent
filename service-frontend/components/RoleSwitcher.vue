<script setup lang="ts">
// Quick Role Switcher — TANPA form login: klik akun demo => login otomatis (TRD 07 §4.1).
import type { Role } from '~/types/api'

const auth = useAuth()
const route = useRoute()
const open = ref(false)
const busy = ref<string | null>(null)
const errorMsg = ref<string | null>(null)

const badge: Record<Exclude<Role, 'GUEST'>, string> = {
  CUSTOMER: 'bg-blue-100 text-blue-800',
  ADMIN_SISTEM: 'bg-orange-100 text-orange-800',
  ADMIN_DEV: 'bg-purple-100 text-purple-800',
}
const home: Record<Exclude<Role, 'GUEST'>, string> = { CUSTOMER: '/', ADMIN_SISTEM: '/admin', ADMIN_DEV: '/dev' }

onMounted(async () => {
  try { await auth.loadDemoUsers() } catch { errorMsg.value = 'Gagal memuat daftar akun demo.' }
})

async function pick(email: string, role: Exclude<Role, 'GUEST'>) {
  busy.value = email
  errorMsg.value = null
  try {
    await auth.loginAs(email)
    open.value = false
    useToast().success(`Masuk sebagai ${auth.user.value?.name} (${role})`)
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : home[role]
    await navigateTo(redirect)
  } catch (e) {
    errorMsg.value = (e as Error).message
  } finally {
    busy.value = null
  }
}

function signOut() {
  auth.logout()
  open.value = false
  void navigateTo('/')
}
</script>

<template>
  <div class="relative">
    <button
      type="button"
      class="flex items-center gap-2 rounded-md border px-3 py-1.5 text-sm hover:bg-gray-50"
      aria-haspopup="menu"
      :aria-expanded="open"
      @click="open = !open"
    >
      <span v-if="auth.user.value">{{ auth.user.value.name }}
        <span class="ml-1 rounded px-1.5 py-0.5 text-xs" :class="badge[auth.user.value.role]">{{ auth.user.value.role }}</span>
      </span>
      <span v-else>Tamu (GUEST)</span>
    </button>

    <div v-if="open" role="menu" class="absolute right-0 z-50 mt-2 w-72 rounded-md border bg-white p-2 shadow-lg">
      <p class="px-2 pb-2 text-xs text-gray-500">Mode demo — akun contoh, tanpa password</p>
      <button
        v-for="u in auth.demoUsers.value"
        :key="u.id"
        type="button"
        role="menuitem"
        class="flex w-full items-center justify-between rounded px-2 py-2 text-left text-sm hover:bg-gray-50 disabled:opacity-50"
        :aria-current="auth.user.value?.id === u.id ? 'true' : undefined"
        :disabled="busy !== null"
        @click="pick(u.email, u.role)"
      >
        <span>{{ u.name }}<br><span class="text-xs text-gray-500">{{ u.email }}</span></span>
        <span class="rounded px-1.5 py-0.5 text-xs" :class="badge[u.role]">{{ busy === u.email ? '…' : u.role }}</span>
      </button>
      <button v-if="auth.isAuthenticated.value" type="button" role="menuitem" class="mt-1 w-full rounded px-2 py-2 text-left text-sm text-red-700 hover:bg-red-50" @click="signOut">
        Keluar (jadi Tamu)
      </button>
      <p v-if="errorMsg" class="px-2 pt-2 text-xs text-red-600">{{ errorMsg }}</p>
    </div>
  </div>
</template>
