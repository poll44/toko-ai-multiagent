<script setup lang="ts">
const cart = useCart()
const auth = useAuth()

function checkout() {
  if (!auth.isAuthenticated.value) {
    useToast().info('Pilih akun demo (Budi/Siti) di pojok kanan atas untuk checkout.')
    return navigateTo({ path: '/', query: { redirect: '/checkout' } })
  }
  return navigateTo('/checkout')
}
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-2xl font-bold">Keranjang</h1>
    <div v-if="!cart.items.value.length" class="rounded-lg border bg-white p-6 text-sm text-gray-600">
      Keranjang masih kosong. <NuxtLink to="/" class="text-indigo-700 underline">Lihat katalog</NuxtLink>
    </div>
    <template v-else>
      <ul class="divide-y rounded-lg border bg-white">
        <li v-for="i in cart.items.value" :key="i.product_id" class="flex flex-wrap items-center justify-between gap-3 p-4">
          <div>
            <NuxtLink :to="`/products/${i.product_id}`" class="font-medium hover:text-indigo-700">{{ i.name }}</NuxtLink>
            <p class="text-sm text-gray-500">{{ idr(i.price) }}</p>
          </div>
          <div class="flex items-center gap-3">
            <input :value="i.quantity" type="number" min="1" max="99" class="w-20 rounded-md border px-2 py-1" @change="cart.setQty(i.product_id, Number(($event.target as HTMLInputElement).value))">
            <span class="w-28 text-right font-semibold">{{ idr(i.price * i.quantity) }}</span>
            <button type="button" class="text-sm text-red-600" @click="cart.remove(i.product_id)">Hapus</button>
          </div>
        </li>
      </ul>
      <div class="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-white p-4">
        <div>
          <p class="text-sm text-gray-500">Total estimasi</p>
          <p class="text-2xl font-bold text-indigo-700">{{ idr(cart.total.value) }}</p>
          <p class="text-xs text-gray-500">Harga final dihitung server saat checkout.</p>
        </div>
        <div class="flex gap-2">
          <button type="button" class="rounded-md border px-3 py-2 text-sm" @click="cart.clear()">Kosongkan</button>
          <button type="button" class="rounded-md bg-indigo-600 px-4 py-2 text-sm text-white" @click="checkout">Checkout</button>
        </div>
      </div>
    </template>
  </div>
</template>
