<script setup lang="ts">
import type { Ticket } from '~/types/api'

const { $apiList } = useApi()
const tickets = ref<Ticket[]>([])
const status = ref('')
const category = ref('')
const loading = ref(true)
const error = ref<string | null>(null)

async function load() {
  loading.value = true
  error.value = null
  try { tickets.value = (await $apiList<Ticket>('/api/tickets', { query: { status: status.value, category: category.value, limit: 100 } })).data } catch (e) { error.value = errText(e) } finally { loading.value = false }
}
onMounted(load)
watch([status, category], load)
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-2xl font-bold">Semua Tiket</h1>
    <div class="flex flex-wrap gap-2">
      <select v-model="status" class="rounded-md border px-3 py-2 text-sm"><option value="">Semua status</option><option v-for="s in ['OPEN', 'IN_PROGRESS', 'RESOLVED', 'CLOSED']" :key="s" :value="s">{{ s }}</option></select>
      <select v-model="category" class="rounded-md border px-3 py-2 text-sm"><option value="">Semua kategori</option><option v-for="c in ['PAYMENT', 'SHIPPING', 'PRODUCT', 'TECHNICAL', 'OTHER']" :key="c" :value="c">{{ c }}</option></select>
    </div>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-else-if="loading" class="text-sm text-gray-500">Memuat…</p>
    <p v-else-if="!tickets.length" class="text-sm text-gray-600">Tidak ada tiket.</p>
    <div v-else class="overflow-x-auto rounded-lg border bg-white">
      <table class="w-full text-left text-sm">
        <thead class="bg-gray-50 text-xs uppercase text-gray-500"><tr><th class="p-3">Tiket</th><th class="p-3">Subjek</th><th class="p-3">Pemilik</th><th class="p-3">Kategori</th><th class="p-3">Prioritas</th><th class="p-3">Status</th></tr></thead>
        <tbody class="divide-y">
          <tr v-for="t in tickets" :key="t.id">
            <td class="p-3"><NuxtLink :to="`/tickets/${t.id}`" class="text-indigo-700 underline">{{ t.id }}</NuxtLink></td>
            <td class="p-3">{{ t.subject }}</td><td class="p-3">{{ t.user_id }}</td><td class="p-3">{{ t.category }}</td>
            <td class="p-3"><StatusBadge kind="priority" :value="t.priority" /></td><td class="p-3"><StatusBadge kind="ticket" :value="t.status" /></td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
