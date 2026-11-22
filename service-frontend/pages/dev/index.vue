<script setup lang="ts">
import type { AuthUser, Ticket } from '~/types/api'

const { $api, $apiList } = useApi()
const config = useRuntimeConfig()

const health = ref<Record<string, string>>({})
const me = ref<AuthUser | null>(null)
const tickets = ref<Ticket[]>([])
const error = ref<string | null>(null)
const checking = ref(false)

async function loadHealth() {
  checking.value = true
  try { health.value = await $api<Record<string, string>>('/api/dev/health') } catch (e) { error.value = errText(e) } finally { checking.value = false }
}

onMounted(async () => {
  await loadHealth()
  try { me.value = await $api<AuthUser>('/auth/me') } catch { /* abaikan */ }
  try { tickets.value = (await $apiList<Ticket>('/api/tickets', { query: { category: 'TECHNICAL', limit: 50 } })).data } catch (e) { error.value = errText(e) }
})

const token = computed(() => tokenStore.get() ?? '')
const jwtClaims = computed(() => {
  try { return JSON.parse(atob(token.value.split('.')[1]!.replace(/-/g, '+').replace(/_/g, '/'))) } catch { return null }
})
const curl = computed(() => `curl ${config.public.apiBase}/api/products -H "Authorization: Bearer <token>"`)
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold">Dashboard Developer</h1>
      <button type="button" class="rounded-md border px-3 py-2 text-sm disabled:opacity-50" :disabled="checking" @click="loadHealth">Periksa ulang</button>
    </div>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>

    <section class="grid gap-3 sm:grid-cols-3 lg:grid-cols-5">
      <div v-for="(st, name) in health" :key="name" class="rounded-lg border bg-white p-4">
        <p class="text-sm text-gray-500">service-{{ name }}</p>
        <p class="mt-1 font-bold" :class="st === 'up' ? 'text-green-600' : 'text-red-600'">{{ st === 'up' ? '● UP' : '● DOWN' }}</p>
      </div>
    </section>

    <section>
      <h2 class="mb-2 font-semibold">Tiket TECHNICAL</h2>
      <p v-if="!tickets.length" class="text-sm text-gray-600">Tidak ada tiket teknis.</p>
      <ul class="space-y-2">
        <li v-for="t in tickets" :key="t.id">
          <NuxtLink :to="`/tickets/${t.id}`" class="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-white p-3 text-sm hover:border-indigo-300">
            <span>{{ t.id }} · {{ t.subject }}</span><StatusBadge kind="ticket" :value="t.status" />
          </NuxtLink>
        </li>
      </ul>
    </section>

    <section class="grid gap-4 lg:grid-cols-2">
      <div class="rounded-lg border bg-white p-4">
        <h2 class="mb-2 font-semibold">Identitas JWT</h2>
        <pre class="overflow-x-auto text-xs">{{ JSON.stringify({ me, claims: jwtClaims }, null, 2) }}</pre>
      </div>
      <div class="rounded-lg border bg-white p-4">
        <h2 class="mb-2 font-semibold">Contoh curl</h2>
        <pre class="overflow-x-auto whitespace-pre-wrap text-xs">{{ curl }}</pre>
        <p class="mt-2 text-xs text-gray-500">Login: POST {{ config.public.apiBase }}/auth/login {"email","password"}</p>
      </div>
    </section>
  </div>
</template>
