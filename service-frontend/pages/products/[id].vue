<script setup lang="ts">
import type { Product } from '~/types/api'

const { $api } = useApi()
const route = useRoute()
const cart = useCart()
const auth = useAuth()
const toast = useToast()
const chat = useChat()

const product = ref<Product | null>(null)
const error = ref<string | null>(null)
const qty = ref(1)
const canBuy = computed(() => !auth.isAuthenticated.value || auth.hasRole('CUSTOMER'))

onMounted(async () => {
  try { product.value = await $api<Product>(`/api/products/${route.params.id}`) } catch (e) { error.value = errText(e) }
})

function add() {
  if (!product.value) return
  cart.add(product.value, qty.value)
  toast.success('Ditambahkan ke keranjang')
}
function askAi() {
  if (!product.value) return
  chat.open.value = true
  void chat.send(`Ceritakan tentang produk ${product.value.id} (${product.value.name}). Apakah cocok untuk saya?`)
}
</script>

<template>
  <div class="space-y-4">
    <NuxtLink to="/" class="text-sm text-indigo-700">‹ Kembali ke katalog</NuxtLink>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-else-if="!product" class="text-sm text-gray-500">Memuat…</p>
    <article v-else class="space-y-4 rounded-lg border bg-white p-6">
      <div class="flex items-start justify-between gap-3">
        <div>
          <h1 class="text-2xl font-bold">{{ product.name }}</h1>
          <p class="text-xs text-gray-500">{{ product.id }} · {{ product.category }}</p>
        </div>
        <StatusBadge kind="stock" :value="product.stock_status" />
      </div>
      <p class="text-3xl font-bold text-indigo-700">{{ idr(product.price) }}</p>
      <p class="text-gray-700">{{ product.description }}</p>
      <section v-if="product.specifications">
        <h2 class="mb-1 font-semibold">Spesifikasi</h2>
        <p class="whitespace-pre-wrap text-sm text-gray-700">{{ product.specifications }}</p>
      </section>
      <p class="text-sm text-gray-600">Stok: {{ product.stock }}</p>
      <div class="flex flex-wrap items-center gap-3">
        <template v-if="canBuy">
          <input v-model.number="qty" type="number" min="1" :max="Math.max(product.stock, 1)" class="w-20 rounded-md border px-2 py-1.5">
          <button type="button" class="rounded-md bg-indigo-600 px-4 py-2 text-sm text-white disabled:bg-gray-300" :disabled="product.stock <= 0" @click="add">+ Keranjang</button>
        </template>
        <button type="button" class="rounded-md border border-indigo-300 px-4 py-2 text-sm text-indigo-700" @click="askAi">✨ Tanya Asisten AI tentang produk ini</button>
      </div>
    </article>
  </div>
</template>
