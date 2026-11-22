<script setup lang="ts">
import type { KnowledgeChunk, Order, Ticket } from '~/types/api'

const { $api, $apiList } = useApi()
const route = useRoute()
const toast = useToast()

const form = reactive({
  subject: '',
  description: '',
  category: 'OTHER' as Ticket['category'],
  priority: 'MEDIUM' as Ticket['priority'],
  order_id: typeof route.query.order_id === 'string' ? route.query.order_id : '',
})
const orders = ref<Order[]>([])
const hints = ref<KnowledgeChunk[]>([])
const busy = ref(false)
const error = ref<string | null>(null)

const valid = computed(() => form.subject.trim().length >= 5 && form.description.trim().length >= 10)
const showHints = computed(() => ['PAYMENT', 'SHIPPING', 'PRODUCT'].includes(form.category))

onMounted(async () => {
  try { orders.value = (await $apiList<Order>('/api/orders', { query: { limit: 50 } })).data } catch { /* dropdown opsional */ }
})

// Saran kebijakan (opsional, tidak memblokir submit): knowledge/search dari subject.
let t: ReturnType<typeof setTimeout> | undefined
watch([() => form.subject, () => form.category], () => {
  clearTimeout(t)
  hints.value = []
  if (!showHints.value || form.subject.trim().length < 3) return
  t = setTimeout(async () => {
    try { hints.value = (await $apiList<KnowledgeChunk>('/api/knowledge/search', { query: { q: form.subject, limit: 2 } })).data } catch { /* abaikan */ }
  }, 500)
})

async function submit() {
  if (!valid.value || busy.value) return
  busy.value = true
  error.value = null
  try {
    const body: Record<string, string> = { subject: form.subject.trim(), description: form.description.trim(), category: form.category, priority: form.priority }
    if (form.order_id) body.order_id = form.order_id
    const ticket = await $api<Ticket>('/api/tickets', { method: 'POST', body })
    toast.success(`Tiket ${ticket.id} dibuat.`)
    await navigateTo(`/tickets/${ticket.id}`)
  } catch (e) { error.value = errText(e) } finally { busy.value = false }
}
</script>

<template>
  <div class="mx-auto max-w-2xl space-y-4">
    <h1 class="text-2xl font-bold">Buat tiket</h1>
    <form class="space-y-4" @submit.prevent="submit">
      <label class="block text-sm"><span class="mb-1 block font-medium">Subjek (min. 5 karakter)</span>
        <input v-model="form.subject" maxlength="150" class="w-full rounded-md border px-3 py-2"></label>
      <div class="grid gap-3 sm:grid-cols-3">
        <label class="block text-sm"><span class="mb-1 block font-medium">Kategori</span>
          <select v-model="form.category" class="w-full rounded-md border px-3 py-2">
            <option v-for="c in ['PAYMENT', 'SHIPPING', 'PRODUCT', 'TECHNICAL', 'OTHER']" :key="c" :value="c">{{ c }}</option>
          </select></label>
        <label class="block text-sm"><span class="mb-1 block font-medium">Prioritas</span>
          <select v-model="form.priority" class="w-full rounded-md border px-3 py-2">
            <option v-for="p in ['LOW', 'MEDIUM', 'HIGH']" :key="p" :value="p">{{ p }}</option>
          </select></label>
        <label class="block text-sm"><span class="mb-1 block font-medium">Pesanan terkait</span>
          <select v-model="form.order_id" class="w-full rounded-md border px-3 py-2">
            <option value="">— tidak ada —</option>
            <option v-for="o in orders" :key="o.id" :value="o.id">{{ o.id }}</option>
          </select></label>
      </div>

      <aside v-if="hints.length" class="rounded-md border border-indigo-200 bg-indigo-50 p-3 text-sm">
        <p class="mb-1 font-medium text-indigo-800">Mungkin ini menjawab pertanyaan Anda</p>
        <div v-for="h in hints" :key="h.chunk_id" class="mb-2">
          <p class="font-medium">{{ h.title }}</p>
          <p class="line-clamp-3 text-gray-700">{{ h.content }}</p>
          <NuxtLink :to="`/help?doc=${h.document_id}`" class="text-xs text-indigo-700 underline">Selengkapnya</NuxtLink>
        </div>
      </aside>

      <label class="block text-sm"><span class="mb-1 block font-medium">Deskripsi (min. 10 karakter)</span>
        <textarea v-model="form.description" rows="5" maxlength="2000" class="w-full rounded-md border px-3 py-2" /></label>
      <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
      <button type="submit" class="w-full rounded-md bg-indigo-600 px-4 py-2.5 text-white disabled:opacity-50" :disabled="!valid || busy">{{ busy ? 'Mengirim…' : 'Kirim tiket' }}</button>
    </form>
  </div>
</template>
