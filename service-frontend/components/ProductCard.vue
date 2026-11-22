<script setup lang="ts">
import type { Product } from '~/types/api'
const props = defineProps<{ product: Product; showMatch?: boolean }>()
const cart = useCart()
const auth = useAuth()
const toast = useToast()
const canBuy = computed(() => !auth.isAuthenticated.value || auth.hasRole('CUSTOMER'))
const match = computed(() => {
  const s = props.product.score
  return s == null ? null : s >= 0.75 ? 'Sangat cocok' : s >= 0.55 ? 'Cocok' : null
})
function add() { cart.add(props.product); toast.success(`${props.product.name} ditambahkan ke keranjang`) }
</script>
<template>
  <article class="flex flex-col rounded-lg border bg-white p-4">
    <div class="flex items-start justify-between gap-2">
      <NuxtLink :to="`/products/${product.id}`" class="font-semibold hover:text-indigo-700">{{ product.name }}</NuxtLink>
      <StatusBadge kind="stock" :value="product.stock_status" />
    </div>
    <p class="mt-1 text-xs text-gray-500">{{ product.id }} · {{ product.category }}</p>
    <p v-if="showMatch && match" class="mt-2 inline-block w-fit rounded bg-indigo-50 px-2 py-0.5 text-xs text-indigo-700">✨ {{ match }}</p>
    <p class="mt-2 flex-1 text-sm text-gray-700">{{ product.description }}</p>
    <p class="mt-3 text-lg font-bold text-indigo-700">{{ idr(product.price) }}</p>
    <p v-if="product.stock_status === 'LOW_STOCK'" class="text-xs text-yellow-700">Stok tinggal {{ product.stock }}</p>
    <button v-if="canBuy" type="button" class="mt-3 rounded-md bg-indigo-600 px-3 py-1.5 text-sm text-white disabled:bg-gray-300" :disabled="product.stock <= 0" @click="add">
      {{ product.stock <= 0 ? 'Stok habis' : '+ Keranjang' }}
    </button>
  </article>
</template>
