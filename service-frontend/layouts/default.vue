<script setup lang="ts">
const auth = useAuth()
const cart = useCart()
const links = computed(() => {
  const base: { to: string; label: string }[] = [{ to: '/', label: 'Katalog' }, { to: '/help', label: 'Bantuan' }]
  if (auth.hasRole('CUSTOMER') || !auth.isAuthenticated.value) base.push({ to: '/cart', label: cart.count.value ? `Keranjang (${cart.count.value})` : 'Keranjang' })
  if (auth.hasRole('CUSTOMER')) base.push({ to: '/orders', label: 'Pesanan' }, { to: '/tickets', label: 'Tiket' })
  if (auth.hasRole('ADMIN_SISTEM')) {
    base.push(
      { to: '/admin', label: 'Admin' }, { to: '/admin/products', label: 'Produk' }, { to: '/admin/orders', label: 'Pesanan' },
      { to: '/admin/tickets', label: 'Tiket' }, { to: '/admin/knowledge', label: 'Knowledge' },
    )
  }
  if (auth.hasRole('ADMIN_DEV')) base.push({ to: '/dev', label: 'Dev' })
  return base
})
</script>

<template>
  <div class="min-h-screen bg-gray-50 text-gray-900">
    <header class="sticky top-0 z-30 border-b bg-white">
      <div class="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-4 px-4 py-3">
        <NuxtLink to="/" class="text-lg font-bold text-indigo-700">Toko AI</NuxtLink>
        <nav class="flex flex-wrap items-center gap-4 text-sm">
          <NuxtLink v-for="l in links" :key="l.to" :to="l.to" exact-active-class="font-semibold text-indigo-700" class="hover:text-indigo-700">{{ l.label }}</NuxtLink>
        </nav>
        <RoleSwitcher />
      </div>
    </header>
    <main class="mx-auto max-w-6xl px-4 py-6">
      <slot />
    </main>
    <ToastHost />
    <ChatWidget />
  </div>
</template>
