<script setup lang="ts">
// Pusat Bantuan: pencarian semantik kebijakan toko (GET /api/knowledge/search) — terbuka untuk GUEST.
import type { ApiMeta, KnowledgeChunk, KnowledgeDocument } from '~/types/api'

const { $apiList, $api } = useApi()
const route = useRoute()
const auth = useAuth()
const chat = useChat()
function askAi() { chat.open.value = true; void chat.send(q.value) }

const q = ref(typeof route.query.q === 'string' ? route.query.q : '')
const results = ref<KnowledgeChunk[]>([])
const meta = ref<ApiMeta>({})
const docs = ref<KnowledgeDocument[]>([])
const openDoc = ref<KnowledgeDocument | null>(null)
const searched = ref(false)
const loading = ref(false)
const error = ref<string | null>(null)

const suggestions = ['syarat retur', 'garansi', 'estimasi pengiriman', 'pembayaran gagal', 'refund']
const canSearch = computed(() => q.value.trim().length >= 3)

async function search() {
  if (!canSearch.value) return
  loading.value = true
  error.value = null
  try {
    const res = await $apiList<KnowledgeChunk>('/api/knowledge/search', { query: { q: q.value, limit: 3 } })
    results.value = res.data
    meta.value = res.meta
    searched.value = true
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function showDoc(id: string) {
  try { openDoc.value = await $api<KnowledgeDocument>(`/api/knowledge/documents/${id}`) } catch (e) { error.value = (e as Error).message }
}

onMounted(async () => {
  try { docs.value = await $api<KnowledgeDocument[]>('/api/knowledge/documents') } catch { /* daftar dokumen opsional */ }
  if (typeof route.query.doc === 'string') await showDoc(route.query.doc)
  if (canSearch.value) await search()
})
</script>

<template>
  <div class="space-y-6">
    <h1 class="text-2xl font-bold">Pusat Bantuan</h1>
    <form class="flex flex-wrap gap-3" @submit.prevent="search">
      <input v-model="q" class="min-w-[260px] flex-1 rounded-md border px-3 py-2" placeholder="Tanyakan kebijakan toko, mis. syarat retur barang rusak">
      <button type="submit" class="rounded-md bg-indigo-600 px-4 py-2 text-sm text-white disabled:opacity-50" :disabled="!canSearch || loading">Cari</button>
    </form>
    <div class="flex flex-wrap gap-2">
      <button v-for="s in suggestions" :key="s" type="button" class="rounded-full border px-3 py-1 text-xs hover:bg-gray-100" @click="q = s; search()">{{ s }}</button>
    </div>

    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-if="meta.mode === 'keyword_fallback'" class="rounded-md bg-yellow-50 p-3 text-sm text-yellow-800">Pencarian cerdas sedang terbatas; hasil memakai pencocokan kata kunci.</p>
    <p v-if="searched && !results.length && !loading" class="text-sm text-gray-600">
      Tidak ada informasi resmi yang cocok. Anda dapat bertanya ke Asisten AI atau membuat tiket.
    </p>
    <div v-if="searched && !loading" class="flex flex-wrap gap-2 text-sm">
      <button type="button" class="rounded-md border border-indigo-300 px-3 py-1.5 text-indigo-700" @click="askAi">✨ Tanya Asisten AI</button>
      <NuxtLink v-if="auth.hasRole('CUSTOMER')" to="/tickets/new" class="rounded-md border px-3 py-1.5">Buat tiket</NuxtLink>
      <span v-else-if="!auth.isAuthenticated.value" class="self-center text-xs text-gray-500">Pilih akun demo untuk membuat tiket.</span>
    </div>

    <article v-for="r in results" :key="r.chunk_id" class="rounded-lg border bg-white p-4">
      <div class="flex items-center justify-between">
        <h3 class="font-semibold">{{ r.title }}</h3>
        <span class="rounded bg-gray-100 px-2 py-0.5 text-xs">{{ r.category }}</span>
      </div>
      <p class="mt-2 whitespace-pre-wrap text-sm text-gray-700">{{ r.content }}</p>
      <button type="button" class="mt-2 text-sm text-indigo-700 underline" @click="showDoc(r.document_id)">Lihat dokumen lengkap</button>
    </article>

    <section v-if="openDoc" class="rounded-lg border-2 border-indigo-200 bg-white p-4">
      <div class="flex items-center justify-between">
        <h2 class="text-lg font-semibold">{{ openDoc.title }}</h2>
        <button type="button" class="text-sm text-gray-500" @click="openDoc = null">Tutup</button>
      </div>
      <p class="mt-2 whitespace-pre-wrap text-sm text-gray-700">{{ openDoc.content }}</p>
    </section>

    <section v-if="docs.length">
      <h2 class="mb-2 font-semibold">Semua topik</h2>
      <ul class="grid gap-2 sm:grid-cols-2">
        <li v-for="d in docs" :key="d.id">
          <button type="button" class="w-full rounded-md border bg-white px-3 py-2 text-left text-sm hover:bg-gray-50" @click="showDoc(d.id)">
            {{ d.title }} <span class="text-xs text-gray-500">· {{ d.category }}</span>
          </button>
        </li>
      </ul>
    </section>
  </div>
</template>
