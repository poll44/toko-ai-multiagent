<script setup lang="ts">
import type { Product } from '~/types/api'

const { $api, $apiList } = useApi()
const toast = useToast()

const products = ref<Product[]>([])
const q = ref('')
const loading = ref(true)
const error = ref<string | null>(null)
const reindexing = ref(false)

// modal produk (tambah/ubah)
const editing = ref<Product | null>(null)
const showForm = ref(false)
const form = reactive({ name: '', description: '', specifications: '', category: '', price: 0, stock: 0, image_url: '' })
const formError = ref<string | null>(null)
const saving = ref(false)

// modal stok
const stockFor = ref<Product | null>(null)
const stockMode = ref<'set' | 'adjust'>('adjust')
const stockValue = ref(0)
const stockNote = ref('')

const toDeactivate = ref<Product | null>(null)

async function load() {
  loading.value = true
  error.value = null
  try { products.value = (await $apiList<Product>('/api/products', { query: { q: q.value, limit: 100 } })).data } catch (e) { error.value = errText(e) } finally { loading.value = false }
}
onMounted(load)

function openForm(p: Product | null) {
  editing.value = p
  Object.assign(form, p
    ? { name: p.name, description: p.description, specifications: p.specifications, category: p.category, price: p.price, stock: p.stock, image_url: p.image_url ?? '' }
    : { name: '', description: '', specifications: '', category: '', price: 0, stock: 0, image_url: '' })
  formError.value = null
  showForm.value = true
}

async function save() {
  saving.value = true
  formError.value = null
  const body = { ...form, image_url: form.image_url.trim() || null }
  try {
    if (editing.value) await $api(`/api/products/${editing.value.id}`, { method: 'PUT', body })
    else await $api('/api/products', { method: 'POST', body })
    toast.success('Produk disimpan.')
    showForm.value = false
    await load()
  } catch (e) { formError.value = errText(e) } finally { saving.value = false }
}

async function saveStock() {
  if (!stockFor.value) return
  try {
    await $api(`/api/products/${stockFor.value.id}/stock`, { method: 'PATCH', body: { mode: stockMode.value, value: stockValue.value, note: stockNote.value || undefined } })
    toast.success('Stok diperbarui.')
    stockFor.value = null
    await load()
  } catch (e) { toast.error(errText(e)) }
}

async function deactivate() {
  const p = toDeactivate.value
  toDeactivate.value = null
  if (!p) return
  try { await $api(`/api/products/${p.id}`, { method: 'DELETE' }); toast.success(`${p.id} dinonaktifkan.`); await load() } catch (e) { toast.error(errText(e)) }
}

async function reindex() {
  reindexing.value = true
  try { await $api('/api/products/reindex', { method: 'POST' }); toast.success('Reindex embedding selesai.') } catch (e) { toast.error(errText(e)) } finally { reindexing.value = false }
}

function openStock(p: Product) { stockFor.value = p; stockMode.value = 'adjust'; stockValue.value = 0; stockNote.value = '' }
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-bold">Kelola Produk</h1>
      <div class="flex gap-2">
        <button type="button" class="rounded-md border px-3 py-2 text-sm disabled:opacity-50" :disabled="reindexing" @click="reindex">{{ reindexing ? 'Memproses…' : 'Reindex embedding' }}</button>
        <button type="button" class="rounded-md bg-indigo-600 px-4 py-2 text-sm text-white" @click="openForm(null)">+ Produk</button>
      </div>
    </div>
    <form class="flex gap-2" @submit.prevent="load">
      <input v-model="q" class="flex-1 rounded-md border px-3 py-2 text-sm" placeholder="Cari produk…">
      <button type="submit" class="rounded-md border px-3 py-2 text-sm">Cari</button>
    </form>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-else-if="loading" class="text-sm text-gray-500">Memuat…</p>
    <div v-else class="overflow-x-auto rounded-lg border bg-white">
      <table class="w-full text-left text-sm">
        <thead class="bg-gray-50 text-xs uppercase text-gray-500"><tr><th class="p-3">ID</th><th class="p-3">Nama</th><th class="p-3">Kategori</th><th class="p-3">Harga</th><th class="p-3">Stok</th><th class="p-3">Status</th><th class="p-3" /></tr></thead>
        <tbody class="divide-y">
          <tr v-for="p in products" :key="p.id" :class="{ 'opacity-50': !p.is_active }">
            <td class="p-3">{{ p.id }}</td><td class="p-3">{{ p.name }}</td><td class="p-3">{{ p.category }}</td><td class="p-3">{{ idr(p.price) }}</td>
            <td class="p-3">{{ p.stock }} <StatusBadge kind="stock" :value="p.stock_status" /></td>
            <td class="p-3">{{ p.is_active ? 'Aktif' : 'Nonaktif' }}</td>
            <td class="space-x-2 whitespace-nowrap p-3 text-right">
              <button type="button" class="text-indigo-700" @click="openForm(p)">Ubah</button>
              <button type="button" class="text-indigo-700" @click="openStock(p)">Atur stok</button>
              <button v-if="p.is_active" type="button" class="text-red-600" @click="toDeactivate = p">Nonaktifkan</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="showForm" class="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/40 p-4">
      <form class="w-full max-w-lg space-y-3 rounded-lg bg-white p-5 shadow-xl" @submit.prevent="save">
        <h2 class="font-semibold">{{ editing ? `Ubah ${editing.id}` : 'Produk baru' }}</h2>
        <input v-model="form.name" required placeholder="Nama" class="w-full rounded-md border px-3 py-2 text-sm">
        <textarea v-model="form.description" required rows="2" placeholder="Deskripsi" class="w-full rounded-md border px-3 py-2 text-sm" />
        <textarea v-model="form.specifications" rows="2" placeholder="Spesifikasi" class="w-full rounded-md border px-3 py-2 text-sm" />
        <div class="grid grid-cols-3 gap-2">
          <input v-model="form.category" required placeholder="Kategori" class="rounded-md border px-3 py-2 text-sm">
          <input v-model.number="form.price" type="number" min="0" required placeholder="Harga" class="rounded-md border px-3 py-2 text-sm">
          <input v-model.number="form.stock" type="number" min="0" required placeholder="Stok" class="rounded-md border px-3 py-2 text-sm">
        </div>
        <input v-model="form.image_url" placeholder="URL gambar (opsional)" class="w-full rounded-md border px-3 py-2 text-sm">
        <p v-if="formError" class="rounded-md bg-red-50 p-2 text-sm text-red-700">{{ formError }}</p>
        <div class="flex justify-end gap-2">
          <button type="button" class="rounded-md border px-3 py-1.5 text-sm" @click="showForm = false">Batal</button>
          <button type="submit" class="rounded-md bg-indigo-600 px-4 py-1.5 text-sm text-white disabled:opacity-50" :disabled="saving">Simpan</button>
        </div>
      </form>
    </div>

    <ConfirmDialog :open="!!stockFor" :title="`Atur stok ${stockFor?.name ?? ''}`" confirm-label="Simpan" @confirm="saveStock" @cancel="stockFor = null">
      <div class="mt-3 space-y-2 text-sm">
        <select v-model="stockMode" class="w-full rounded-md border px-3 py-2"><option value="adjust">Tambah/kurangi (+/−)</option><option value="set">Set jumlah pasti</option></select>
        <input v-model.number="stockValue" type="number" class="w-full rounded-md border px-3 py-2" placeholder="Nilai">
        <input v-model="stockNote" maxlength="200" class="w-full rounded-md border px-3 py-2" placeholder="Catatan (opsional)">
      </div>
    </ConfirmDialog>
    <ConfirmDialog :open="!!toDeactivate" title="Nonaktifkan produk?" :message="`${toDeactivate?.name} tidak akan tampil di katalog.`" confirm-label="Nonaktifkan" danger @confirm="deactivate" @cancel="toDeactivate = null" />
  </div>
</template>
