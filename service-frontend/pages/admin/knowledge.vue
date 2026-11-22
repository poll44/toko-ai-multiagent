<script setup lang="ts">
import type { KnowledgeCategory, KnowledgeDocument } from '~/types/api'

const { $api } = useApi()
const toast = useToast()

const docs = ref<KnowledgeDocument[]>([])
const loading = ref(true)
const error = ref<string | null>(null)
const reindexing = ref(false)

const showForm = ref(false)
const editing = ref<KnowledgeDocument | null>(null)
const form = reactive({ title: '', category: 'GENERAL' as KnowledgeCategory, content: '', is_active: true })
const formError = ref<string | null>(null)
const saving = ref(false)
const toDelete = ref<KnowledgeDocument | null>(null)

const CATEGORIES: KnowledgeCategory[] = ['RETURN', 'WARRANTY', 'SHIPPING', 'PAYMENT', 'ORDER', 'COMPLAINT', 'GENERAL']

async function load() {
  loading.value = true
  error.value = null
  try { docs.value = await $api<KnowledgeDocument[]>('/api/knowledge/documents?include_inactive=true') } catch (e) { error.value = errText(e) } finally { loading.value = false }
}
onMounted(load)

async function openForm(d: KnowledgeDocument | null) {
  formError.value = null
  editing.value = d
  if (d) {
    try {
      const full = await $api<KnowledgeDocument>(`/api/knowledge/documents/${d.id}`) // list tidak membawa content
      Object.assign(form, { title: full.title, category: full.category, content: full.content ?? '', is_active: full.is_active })
    } catch (e) { toast.error(errText(e)); return }
  } else Object.assign(form, { title: '', category: 'GENERAL', content: '', is_active: true })
  showForm.value = true
}

async function save() {
  saving.value = true
  formError.value = null
  try {
    if (editing.value) await $api(`/api/knowledge/documents/${editing.value.id}`, { method: 'PUT', body: { ...form } })
    else await $api('/api/knowledge/documents', { method: 'POST', body: { ...form } })
    toast.success('Dokumen disimpan.')
    showForm.value = false
    await load()
  } catch (e) { formError.value = errText(e) } finally { saving.value = false }
}

async function remove() {
  const d = toDelete.value
  toDelete.value = null
  if (!d) return
  try { await $api(`/api/knowledge/documents/${d.id}`, { method: 'DELETE' }); toast.success(`${d.id} dinonaktifkan.`); await load() } catch (e) { toast.error(errText(e)) }
}

async function reindex() {
  reindexing.value = true
  try { await $api('/api/knowledge/reindex', { method: 'POST' }); toast.success('Reindex knowledge selesai.'); await load() } catch (e) { toast.error(errText(e)) } finally { reindexing.value = false }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">Knowledge Base</h1>
      <div class="flex gap-2">
        <button type="button" class="rounded-md border px-3 py-2 text-sm disabled:opacity-50" :disabled="reindexing" @click="reindex">{{ reindexing ? 'Memproses…' : 'Reindex' }}</button>
        <button type="button" class="rounded-md bg-indigo-600 px-4 py-2 text-sm text-white" @click="openForm(null)">+ Dokumen</button>
      </div>
    </div>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-else-if="loading" class="text-sm text-gray-500">Memuat…</p>
    <div v-else class="overflow-x-auto rounded-lg border bg-white">
      <table class="w-full text-left text-sm">
        <thead class="bg-gray-50 text-xs uppercase text-gray-500"><tr><th class="p-3">ID</th><th class="p-3">Judul</th><th class="p-3">Kategori</th><th class="p-3">Aktif</th><th class="p-3">Chunk</th><th class="p-3" /></tr></thead>
        <tbody class="divide-y">
          <tr v-for="d in docs" :key="d.id" :class="{ 'opacity-50': !d.is_active }">
            <td class="p-3">{{ d.id }}</td><td class="p-3">{{ d.title }}</td><td class="p-3">{{ d.category }}</td><td class="p-3">{{ d.is_active ? 'Ya' : 'Tidak' }}</td><td class="p-3">{{ d.chunk_count }}</td>
            <td class="space-x-2 whitespace-nowrap p-3 text-right">
              <button type="button" class="text-indigo-700" @click="openForm(d)">Ubah</button>
              <button v-if="d.is_active" type="button" class="text-red-600" @click="toDelete = d">Nonaktifkan</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="showForm" class="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/40 p-4">
      <form class="w-full max-w-2xl space-y-3 rounded-lg bg-white p-5 shadow-xl" @submit.prevent="save">
        <h2 class="font-semibold">{{ editing ? `Ubah ${editing.id}` : 'Dokumen baru' }}</h2>
        <input v-model="form.title" required placeholder="Judul" class="w-full rounded-md border px-3 py-2 text-sm">
        <div class="flex gap-3">
          <select v-model="form.category" class="rounded-md border px-3 py-2 text-sm"><option v-for="c in CATEGORIES" :key="c" :value="c">{{ c }}</option></select>
          <label class="flex items-center gap-2 text-sm"><input v-model="form.is_active" type="checkbox"> Aktif</label>
        </div>
        <textarea v-model="form.content" required rows="12" placeholder="Isi dokumen (Markdown)" class="w-full rounded-md border px-3 py-2 font-mono text-sm" />
        <p v-if="formError" class="rounded-md bg-red-50 p-2 text-sm text-red-700">{{ formError }}</p>
        <div class="flex justify-end gap-2">
          <button type="button" class="rounded-md border px-3 py-1.5 text-sm" @click="showForm = false">Batal</button>
          <button type="submit" class="rounded-md bg-indigo-600 px-4 py-1.5 text-sm text-white disabled:opacity-50" :disabled="saving">Simpan</button>
        </div>
      </form>
    </div>
    <ConfirmDialog :open="!!toDelete" title="Nonaktifkan dokumen?" :message="`${toDelete?.title} tidak lagi dipakai untuk pencarian kebijakan.`" confirm-label="Nonaktifkan" danger @confirm="remove" @cancel="toDelete = null" />
  </div>
</template>
