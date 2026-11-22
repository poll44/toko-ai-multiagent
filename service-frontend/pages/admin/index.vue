<script setup lang="ts">
const { $apiList } = useApi()

interface Stat { label: string; value: number | null; to: string; tone?: string }
const stats = ref<Stat[]>([
  { label: 'Produk', value: null, to: '/admin/products' },
  { label: 'Pesanan dibayar (perlu diproses)', value: null, to: '/admin/orders', tone: 'text-blue-700' },
  { label: 'Menunggu pembayaran', value: null, to: '/admin/orders', tone: 'text-yellow-700' },
  { label: 'Tiket terbuka', value: null, to: '/admin/tickets', tone: 'text-red-700' },
  { label: 'Tiket diproses', value: null, to: '/admin/tickets' },
  { label: 'Dokumen knowledge', value: null, to: '/admin/knowledge' },
])

async function total(path: string, query: Record<string, string | number> = {}) {
  try { return (await $apiList(path, { query: { ...query, limit: 1 } })).meta.total ?? 0 } catch { return null }
}

onMounted(async () => {
  const r = await Promise.all([
    total('/api/products'), total('/api/orders', { status: 'PAID' }), total('/api/orders', { status: 'PENDING_PAYMENT' }),
    total('/api/tickets', { status: 'OPEN' }), total('/api/tickets', { status: 'IN_PROGRESS' }),
    $apiList('/api/knowledge/documents').then((x) => x.data.length).catch(() => null),
  ])
  r.forEach((v, i) => { stats.value[i]!.value = v })
})
</script>

<template>
  <div class="space-y-4">
    <h1 class="text-2xl font-bold">Dashboard Admin</h1>
    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <NuxtLink v-for="s in stats" :key="s.label" :to="s.to" class="rounded-lg border bg-white p-5 hover:border-indigo-300">
        <p class="text-sm text-gray-500">{{ s.label }}</p>
        <p class="mt-1 text-3xl font-bold" :class="s.tone">{{ s.value ?? '…' }}</p>
      </NuxtLink>
    </div>
  </div>
</template>
