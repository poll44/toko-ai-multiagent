<script setup lang="ts">
import type { Order } from '~/types/api'

const { $api, $apiList } = useApi()
const toast = useToast()

const orders = ref<Order[]>([])
const status = ref('')
const userId = ref('')
const loading = ref(true)
const error = ref<string | null>(null)

const target = ref<Order | null>(null)
const next = ref<'PROCESSING' | 'SHIPPED' | 'DELIVERED'>('PROCESSING')
const tracking = ref('')
const note = ref('')

const STATUSES = ['PENDING_PAYMENT', 'PAID', 'PROCESSING', 'SHIPPED', 'DELIVERED', 'CANCELLED', 'PAYMENT_FAILED', 'PAYMENT_TIMEOUT']
// langkah fulfilment yang masuk akal berikutnya
const NEXT: Record<string, 'PROCESSING' | 'SHIPPED' | 'DELIVERED' | undefined> = { PAID: 'PROCESSING', PROCESSING: 'SHIPPED', SHIPPED: 'DELIVERED' }

async function load() {
  loading.value = true
  error.value = null
  try { orders.value = (await $apiList<Order>('/api/orders', { query: { status: status.value, user_id: userId.value.trim(), limit: 100 } })).data } catch (e) { error.value = errText(e) } finally { loading.value = false }
}
onMounted(load)
watch(status, load)

function openUpdate(o: Order) { target.value = o; next.value = NEXT[o.status] ?? 'PROCESSING'; tracking.value = ''; note.value = '' }

async function submit() {
  if (!target.value) return
  try {
    await $api(`/api/orders/${target.value.id}/status`, {
      method: 'PATCH',
      body: { status: next.value, tracking_number: next.value === 'SHIPPED' ? tracking.value.trim() : undefined, note: note.value.trim() || undefined },
    })
    toast.success(`${target.value.id} → ${next.value}`)
    target.value = null
    await load()
  } catch (e) { toast.error(errText(e)) }
}
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-2xl font-bold">Semua Pesanan</h1>
    <form class="flex flex-wrap gap-2" @submit.prevent="load">
      <select v-model="status" class="rounded-md border px-3 py-2 text-sm"><option value="">Semua status</option><option v-for="s in STATUSES" :key="s" :value="s">{{ s }}</option></select>
      <input v-model="userId" class="rounded-md border px-3 py-2 text-sm" placeholder="Filter user_id">
      <button type="submit" class="rounded-md border px-3 py-2 text-sm">Terapkan</button>
    </form>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-else-if="loading" class="text-sm text-gray-500">Memuat…</p>
    <p v-else-if="!orders.length" class="text-sm text-gray-600">Tidak ada pesanan.</p>
    <div v-else class="overflow-x-auto rounded-lg border bg-white">
      <table class="w-full text-left text-sm">
        <thead class="bg-gray-50 text-xs uppercase text-gray-500"><tr><th class="p-3">Pesanan</th><th class="p-3">Pemilik</th><th class="p-3">Total</th><th class="p-3">Status</th><th class="p-3">Dibuat</th><th class="p-3" /></tr></thead>
        <tbody class="divide-y">
          <tr v-for="o in orders" :key="o.id">
            <td class="p-3"><NuxtLink :to="`/orders/${o.id}`" class="text-indigo-700 underline">{{ o.id }}</NuxtLink></td>
            <td class="p-3">{{ o.user_id }}</td><td class="p-3">{{ idr(o.total_amount) }}</td>
            <td class="p-3"><StatusBadge kind="order" :value="o.status" /></td><td class="p-3">{{ fmtDate(o.created_at) }}</td>
            <td class="p-3 text-right"><button v-if="NEXT[o.status]" type="button" class="text-indigo-700" @click="openUpdate(o)">Ubah status</button></td>
          </tr>
        </tbody>
      </table>
    </div>

    <ConfirmDialog :open="!!target" :title="`Ubah status ${target?.id ?? ''}`" confirm-label="Simpan" @confirm="submit" @cancel="target = null">
      <div class="mt-3 space-y-2 text-sm">
        <select v-model="next" class="w-full rounded-md border px-3 py-2"><option>PROCESSING</option><option>SHIPPED</option><option>DELIVERED</option></select>
        <input v-if="next === 'SHIPPED'" v-model="tracking" maxlength="40" class="w-full rounded-md border px-3 py-2" placeholder="Nomor resi (5–40 karakter)">
        <input v-model="note" maxlength="200" class="w-full rounded-md border px-3 py-2" placeholder="Catatan (opsional)">
      </div>
    </ConfirmDialog>
  </div>
</template>
