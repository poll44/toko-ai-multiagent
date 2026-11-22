<script setup lang="ts">
import type { Order } from '~/types/api'

const { $apiList } = useApi()
const orders = ref<Order[]>([])
const status = ref('')
const loading = ref(true)
const error = ref<string | null>(null)

const STATUSES = ['PENDING_PAYMENT', 'PAID', 'PROCESSING', 'SHIPPED', 'DELIVERED', 'CANCELLED', 'PAYMENT_FAILED', 'PAYMENT_TIMEOUT']

async function load() {
  loading.value = true
  error.value = null
  try { orders.value = (await $apiList<Order>('/api/orders', { query: { status: status.value, limit: 50 } })).data } catch (e) { error.value = errText(e) } finally { loading.value = false }
}
onMounted(load)
watch(status, load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">Pesanan saya</h1>
      <select v-model="status" class="rounded-md border px-3 py-2 text-sm">
        <option value="">Semua status</option>
        <option v-for="s in STATUSES" :key="s" :value="s">{{ s }}</option>
      </select>
    </div>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-else-if="loading" class="text-sm text-gray-500">Memuat…</p>
    <p v-else-if="!orders.length" class="rounded-lg border bg-white p-6 text-sm text-gray-600">Belum ada pesanan. <NuxtLink to="/" class="text-indigo-700 underline">Mulai belanja</NuxtLink></p>
    <ul v-else class="space-y-3">
      <li v-for="o in orders" :key="o.id">
        <NuxtLink :to="`/orders/${o.id}`" class="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-white p-4 hover:border-indigo-300">
          <div>
            <p class="font-semibold">{{ o.id }}</p>
            <p class="text-xs text-gray-500">{{ fmtDate(o.created_at) }} · {{ o.items.length }} item</p>
          </div>
          <div class="flex items-center gap-3"><span class="font-semibold">{{ idr(o.total_amount) }}</span><StatusBadge kind="order" :value="o.status" /></div>
        </NuxtLink>
      </li>
    </ul>
  </div>
</template>
