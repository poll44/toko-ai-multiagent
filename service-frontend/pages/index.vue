<script setup lang="ts">
// Katalog: pencarian eksak (GET /api/products) dan "Pencarian AI" semantik (GET /api/products/semantic-search).
import type { ApiMeta, Product } from '~/types/api'

const { $apiList, $api } = useApi()

const aiMode = ref(false)
const q = ref('')
const category = ref('')
const inStock = ref(false)
const sort = ref('name')
const page = ref(1)
const products = ref<Product[]>([])
const categories = ref<string[]>([])
const meta = ref<ApiMeta>({})
const loading = ref(false)
const error = ref<string | null>(null)
const LIMIT = 12

async function search() {
  loading.value = true
  error.value = null
  try {
    const common = { q: q.value, category: category.value, in_stock: inStock.value || undefined }
    const res = aiMode.value
      ? await $apiList<Product>('/api/products/semantic-search', { query: { ...common, limit: 8 } })
      : await $apiList<Product>('/api/products', { query: { ...common, sort: sort.value, page: page.value, limit: LIMIT } })
    products.value = res.data
    meta.value = res.meta
  } catch (e) {
    error.value = errText(e)
    products.value = []
  } finally {
    loading.value = false
  }
}

const totalPages = computed(() => (aiMode.value ? 1 : Math.max(1, Math.ceil((meta.value.total ?? 0) / LIMIT))))
// pencarian semantik wajib minimal 3 karakter (validasi backend)
const canSearch = computed(() => !aiMode.value || q.value.trim().length >= 3)
function submit() { page.value = 1; if (canSearch.value) void search() }

onMounted(async () => {
  try { categories.value = (await $api<{ category: string; count: number }[]>('/api/products/categories')).map((c) => c.category) } catch { /* filter opsional */ }
  await search()
})
watch([aiMode, category, inStock, sort], () => { page.value = 1; if (canSearch.value) void search() })
watch(page, search)
</script>

<template>
  <div class="space-y-6">
    <form class="flex flex-wrap items-end gap-3 rounded-lg border bg-white p-4" @submit.prevent="submit">
      <label class="flex min-w-[260px] flex-1 flex-col text-sm">
        <span class="mb-1 font-medium">{{ aiMode ? 'Ceritakan kebutuhan Anda' : 'Cari produk' }}</span>
        <input v-model="q" class="rounded-md border px-3 py-2" :placeholder="aiMode ? 'mis. mouse yang tidak bikin pergelangan pegal' : 'mis. powerbank'">
      </label>
      <label class="flex flex-col text-sm">
        <span class="mb-1 font-medium">Kategori</span>
        <select v-model="category" class="rounded-md border px-3 py-2">
          <option value="">Semua</option>
          <option v-for="c in categories" :key="c" :value="c">{{ c }}</option>
        </select>
      </label>
      <label v-if="!aiMode" class="flex flex-col text-sm">
        <span class="mb-1 font-medium">Urutkan</span>
        <select v-model="sort" class="rounded-md border px-3 py-2">
          <option value="name">Nama</option><option value="price_asc">Harga ↑</option><option value="price_desc">Harga ↓</option><option value="newest">Terbaru</option>
        </select>
      </label>
      <label class="flex items-center gap-2 text-sm"><input v-model="inStock" type="checkbox"> Hanya yang tersedia</label>
      <label class="flex items-center gap-2 text-sm font-medium text-indigo-700"><input v-model="aiMode" type="checkbox"> ✨ Pencarian AI</label>
      <button type="submit" class="rounded-md bg-indigo-600 px-4 py-2 text-sm text-white disabled:opacity-50" :disabled="!canSearch || loading">Cari</button>
    </form>

    <p v-if="aiMode && meta.mode === 'keyword_fallback'" class="rounded-md bg-yellow-50 p-3 text-sm text-yellow-800">
      Pencarian cerdas sedang terbatas; hasil di bawah memakai pencocokan kata kunci dan mungkin kurang tepat.
    </p>
    <p v-if="aiMode && !canSearch" class="text-sm text-gray-500">Tulis minimal 3 karakter untuk Pencarian AI.</p>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-if="loading" class="text-sm text-gray-500">Memuat…</p>
    <p v-else-if="!products.length && !error" class="text-sm text-gray-600">
      {{ aiMode ? 'Belum ada produk yang cocok dengan kebutuhan Anda. Coba kalimat lain atau matikan Pencarian AI.' : 'Belum ada produk yang cocok.' }}
    </p>

    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <ProductCard v-for="p in products" :key="p.id" :product="p" :show-match="aiMode" />
    </div>

    <div v-if="totalPages > 1" class="flex items-center justify-center gap-3 text-sm">
      <button type="button" class="rounded border px-3 py-1 disabled:opacity-40" :disabled="page <= 1" @click="page--">‹ Sebelumnya</button>
      <span>Halaman {{ page }} / {{ totalPages }}</span>
      <button type="button" class="rounded border px-3 py-1 disabled:opacity-40" :disabled="page >= totalPages" @click="page++">Berikutnya ›</button>
    </div>
  </div>
</template>
