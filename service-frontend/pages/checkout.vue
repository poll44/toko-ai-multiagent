<script setup lang="ts">
import type { Order } from '~/types/api'

const { $api } = useApi()
const cart = useCart()
const toast = useToast()

const address = ref('')
const method = ref<Order['payment_method']>('EWALLET')
const busy = ref(false)
const error = ref<string | null>(null)
const stockIssue = ref<{ product_id: string; available: number; requested: number } | null>(null)

const addrValid = computed(() => address.value.trim().length >= 10 && address.value.trim().length <= 300)

async function submit() {
  if (!addrValid.value || busy.value) return
  busy.value = true
  error.value = null
  stockIssue.value = null
  try {
    const order = await $api<Order>('/api/orders', {
      method: 'POST',
      body: {
        items: cart.items.value.map((i) => ({ product_id: i.product_id, quantity: i.quantity })),
        payment_method: method.value,
        shipping_address: address.value.trim(),
      },
    })
    cart.clear()
    toast.success(`Pesanan ${order.id} dibuat. Selesaikan pembayaran.`)
    await navigateTo(`/orders/${order.id}`)
  } catch (e) {
    const err = e as { status?: number; code?: string; details?: any }
    if (err.status === 409 && err.code === 'INSUFFICIENT_STOCK' && err.details) stockIssue.value = err.details
    error.value = errText(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="mx-auto max-w-2xl space-y-4">
    <h1 class="text-2xl font-bold">Checkout</h1>
    <div v-if="!cart.items.value.length" class="rounded-lg border bg-white p-6 text-sm text-gray-600">
      Keranjang kosong. <NuxtLink to="/" class="text-indigo-700 underline">Lihat katalog</NuxtLink>
    </div>
    <form v-else class="space-y-4" @submit.prevent="submit">
      <ul class="divide-y rounded-lg border bg-white text-sm">
        <li v-for="i in cart.items.value" :key="i.product_id" class="flex justify-between p-3">
          <span>{{ i.name }} × {{ i.quantity }}</span><span>{{ idr(i.price * i.quantity) }}</span>
        </li>
        <li class="flex justify-between p-3 font-bold"><span>Total estimasi</span><span>{{ idr(cart.total.value) }}</span></li>
      </ul>
      <label class="block text-sm">
        <span class="mb-1 block font-medium">Alamat pengiriman (10–300 karakter)</span>
        <textarea v-model="address" rows="3" maxlength="300" class="w-full rounded-md border px-3 py-2" placeholder="Jl. Merdeka No. 10, Bandung" />
      </label>
      <label class="block text-sm">
        <span class="mb-1 block font-medium">Metode pembayaran</span>
        <select v-model="method" class="w-full rounded-md border px-3 py-2">
          <option value="EWALLET">E-Wallet</option><option value="BANK_TRANSFER">Transfer Bank</option><option value="CREDIT_CARD">Kartu Kredit</option>
        </select>
      </label>
      <p v-if="stockIssue" class="rounded-md bg-yellow-50 p-3 text-sm text-yellow-800">
        Stok {{ stockIssue.product_id }} tidak cukup: tersedia {{ stockIssue.available }}, diminta {{ stockIssue.requested }}. Ubah jumlah di keranjang.
      </p>
      <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
      <button type="submit" class="w-full rounded-md bg-indigo-600 px-4 py-2.5 text-white disabled:opacity-50" :disabled="!addrValid || busy">{{ busy ? 'Memproses…' : 'Buat pesanan' }}</button>
    </form>
  </div>
</template>
