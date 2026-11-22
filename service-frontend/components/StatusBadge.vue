<script setup lang="ts">
// Badge status generik: kind = order | ticket | stock | priority.
const props = defineProps<{ kind: 'order' | 'ticket' | 'stock' | 'priority'; value: string }>()
const MAP: Record<string, Record<string, [string, string]>> = {
  order: {
    PENDING_PAYMENT: ['Menunggu pembayaran', 'bg-yellow-100 text-yellow-800'], PAID: ['Dibayar', 'bg-blue-100 text-blue-800'],
    PROCESSING: ['Diproses', 'bg-indigo-100 text-indigo-800'], SHIPPED: ['Dikirim', 'bg-purple-100 text-purple-800'],
    DELIVERED: ['Selesai', 'bg-green-100 text-green-800'], CANCELLED: ['Dibatalkan', 'bg-gray-200 text-gray-700'],
    PAYMENT_FAILED: ['Pembayaran gagal', 'bg-red-100 text-red-800'], PAYMENT_TIMEOUT: ['Pembayaran kedaluwarsa', 'bg-red-100 text-red-800'],
  },
  ticket: {
    OPEN: ['Terbuka', 'bg-blue-100 text-blue-800'], IN_PROGRESS: ['Diproses', 'bg-yellow-100 text-yellow-800'],
    RESOLVED: ['Selesai', 'bg-green-100 text-green-800'], CLOSED: ['Ditutup', 'bg-gray-200 text-gray-700'],
  },
  stock: { IN_STOCK: ['Tersedia', 'bg-green-100 text-green-800'], LOW_STOCK: ['Stok menipis', 'bg-yellow-100 text-yellow-800'], OUT_OF_STOCK: ['Habis', 'bg-red-100 text-red-800'] },
  priority: { LOW: ['Rendah', 'bg-gray-100 text-gray-700'], MEDIUM: ['Sedang', 'bg-yellow-100 text-yellow-800'], HIGH: ['Tinggi', 'bg-red-100 text-red-800'] },
}
const entry = computed(() => MAP[props.kind]?.[props.value] ?? [props.value, 'bg-gray-100 text-gray-700'])
</script>
<template>
  <span class="inline-block shrink-0 rounded px-2 py-0.5 text-xs" :class="entry[1]">{{ entry[0] }}</span>
</template>
