<script setup lang="ts">
import type { Ticket } from '~/types/api'

const { $apiList } = useApi()
const tickets = ref<Ticket[]>([])
const status = ref('')
const loading = ref(true)
const error = ref<string | null>(null)

async function load() {
  loading.value = true
  error.value = null
  try { tickets.value = (await $apiList<Ticket>('/api/tickets', { query: { status: status.value, limit: 50 } })).data } catch (e) { error.value = errText(e) } finally { loading.value = false }
}
onMounted(load)
watch(status, load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">Tiket saya</h1>
      <div class="flex gap-2">
        <select v-model="status" class="rounded-md border px-3 py-2 text-sm">
          <option value="">Semua status</option>
          <option v-for="s in ['OPEN', 'IN_PROGRESS', 'RESOLVED', 'CLOSED']" :key="s" :value="s">{{ s }}</option>
        </select>
        <NuxtLink to="/tickets/new" class="rounded-md bg-indigo-600 px-4 py-2 text-sm text-white">+ Tiket baru</NuxtLink>
      </div>
    </div>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-else-if="loading" class="text-sm text-gray-500">Memuat…</p>
    <p v-else-if="!tickets.length" class="rounded-lg border bg-white p-6 text-sm text-gray-600">Belum ada tiket.</p>
    <ul v-else class="space-y-3">
      <li v-for="t in tickets" :key="t.id">
        <NuxtLink :to="`/tickets/${t.id}`" class="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-white p-4 hover:border-indigo-300">
          <div>
            <p class="font-semibold">{{ t.subject }}</p>
            <p class="text-xs text-gray-500">{{ t.id }} · {{ t.category }} · {{ fmtDate(t.created_at) }}<template v-if="t.order_id"> · {{ t.order_id }}</template></p>
          </div>
          <div class="flex items-center gap-2"><StatusBadge kind="priority" :value="t.priority" /><StatusBadge kind="ticket" :value="t.status" /></div>
        </NuxtLink>
      </li>
    </ul>
  </div>
</template>
