<script setup lang="ts">
import type { Order } from '~/types/api'

const { $api } = useApi()
const route = useRoute()
const auth = useAuth()
const toast = useToast()

const order = ref<Order | null>(null)
const error = ref<string | null>(null)
const busy = ref(false)
const confirmCancel = ref(false)
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined

const isOwnerCustomer = computed(() => auth.hasRole('CUSTOMER'))
const pending = computed(() => order.value?.status === 'PENDING_PAYMENT')
const remaining = computed(() => {
  const exp = order.value?.payment_expires_at
  if (!exp || !pending.value) return null
  const s = Math.max(0, Math.floor((new Date(exp).getTime() - now.value) / 1000))
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`
})

async function load() {
  try { order.value = await $api<Order>(`/api/orders/${route.params.id}`) } catch (e) { error.value = errText(e) }
}

async function act(path: string, body: unknown, okMsg: string) {
  busy.value = true
  try {
    order.value = await $api<Order>(`/api/orders/${route.params.id}/${path}`, { method: 'POST', body })
    toast.success(okMsg)
    await load() // muat ulang agar riwayat status ikut terbarui
  } catch (e) { toast.error(errText(e)) } finally { busy.value = false }
}

const pay = (simulate: 'success' | 'fail' | 'timeout') =>
  act('pay', simulate === 'fail' ? { simulate, fail_reason: 'Saldo tidak cukup (simulasi)' } : { simulate }, simulate === 'success' ? 'Pembayaran berhasil.' : 'Simulasi dijalankan.')
const cancel = () => { confirmCancel.value = false; return act('cancel', { reason: 'Dibatalkan oleh pelanggan' }, 'Pesanan dibatalkan.') }

onMounted(async () => {
  await load()
  timer = setInterval(() => { now.value = Date.now() }, 1000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="space-y-4">
    <NuxtLink :to="auth.hasRole('ADMIN_SISTEM') ? '/admin/orders' : '/orders'" class="text-sm text-indigo-700">‹ Kembali</NuxtLink>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-else-if="!order" class="text-sm text-gray-500">Memuat…</p>
    <template v-else>
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-2xl font-bold">{{ order.id }}</h1>
          <p class="text-xs text-gray-500">Dibuat {{ fmtDate(order.created_at) }} · pemilik {{ order.user_id }}</p>
        </div>
        <StatusBadge kind="order" :value="order.status" />
      </div>

      <p v-if="pending && remaining" class="rounded-md bg-yellow-50 p-3 text-sm text-yellow-800">Selesaikan pembayaran dalam <b>{{ remaining }}</b> sebelum pesanan kedaluwarsa.</p>
      <div v-if="order.failure_reason" class="rounded-md bg-red-50 p-3 text-sm text-red-700">
        Alasan: {{ order.failure_reason }}
        <NuxtLink v-if="order.status === 'PAYMENT_FAILED' || order.status === 'PAYMENT_TIMEOUT'" :to="{ path: '/help', query: { q: 'pembayaran gagal' } }" class="ml-2 underline">Baca kebijakan pembayaran</NuxtLink>
      </div>

      <section class="rounded-lg border bg-white">
        <ul class="divide-y text-sm">
          <li v-for="i in order.items" :key="i.product_id" class="flex justify-between p-3">
            <span><NuxtLink :to="`/products/${i.product_id}`" class="hover:text-indigo-700">{{ i.product_name }}</NuxtLink> × {{ i.quantity }} <span class="text-gray-500">({{ idr(i.unit_price) }})</span></span>
            <span>{{ idr(i.subtotal) }}</span>
          </li>
          <li class="flex justify-between p-3 font-bold"><span>Total</span><span>{{ idr(order.total_amount) }}</span></li>
        </ul>
      </section>

      <section class="grid gap-3 rounded-lg border bg-white p-4 text-sm sm:grid-cols-2">
        <p><span class="text-gray-500">Alamat:</span> {{ order.shipping_address }}</p>
        <p><span class="text-gray-500">Pembayaran:</span> {{ order.payment_method }} · {{ order.payment_status }}</p>
        <p v-if="order.tracking_number"><span class="text-gray-500">No. resi:</span> {{ order.tracking_number }}</p>
        <p v-if="order.paid_at"><span class="text-gray-500">Dibayar:</span> {{ fmtDate(order.paid_at) }}</p>
      </section>

      <section v-if="isOwnerCustomer" class="flex flex-wrap gap-2">
        <template v-if="pending">
          <button type="button" class="rounded-md bg-green-600 px-4 py-2 text-sm text-white disabled:opacity-50" :disabled="busy" @click="pay('success')">Bayar (sukses)</button>
          <button type="button" class="rounded-md border border-red-300 px-4 py-2 text-sm text-red-700 disabled:opacity-50" :disabled="busy" @click="confirmCancel = true">Batalkan</button>
        </template>
        <NuxtLink :to="{ path: '/tickets/new', query: { order_id: order.id } }" class="rounded-md border px-4 py-2 text-sm">Buat tiket</NuxtLink>
      </section>

      <section v-if="isOwnerCustomer && pending" class="rounded-lg border border-dashed bg-gray-50 p-4 text-sm">
        <p class="mb-2 font-medium text-gray-700">Panel Demo — simulasi hasil pembayaran</p>
        <div class="flex gap-2">
          <button type="button" class="rounded border bg-white px-3 py-1.5 disabled:opacity-50" :disabled="busy" @click="pay('fail')">Simulasi gagal</button>
          <button type="button" class="rounded border bg-white px-3 py-1.5 disabled:opacity-50" :disabled="busy" @click="pay('timeout')">Simulasi timeout</button>
        </div>
      </section>

      <section v-if="order.history?.length">
        <h2 class="mb-2 font-semibold">Riwayat status</h2>
        <StatusTimeline :history="order.history" />
      </section>

      <ConfirmDialog :open="confirmCancel" title="Batalkan pesanan?" message="Stok akan dikembalikan dan tindakan ini tidak dapat diurungkan." confirm-label="Ya, batalkan" danger @confirm="cancel" @cancel="confirmCancel = false" />
    </template>
  </div>
</template>
