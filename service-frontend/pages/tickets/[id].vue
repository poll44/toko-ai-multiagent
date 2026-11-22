<script setup lang="ts">
import type { Ticket } from '~/types/api'

const { $api } = useApi()
const route = useRoute()
const auth = useAuth()
const toast = useToast()

const ticket = ref<Ticket | null>(null)
const error = ref<string | null>(null)
const reply = ref('')
const note = ref('')
const busy = ref(false)
const confirmClose = ref(false)

const isCustomer = computed(() => auth.hasRole('CUSTOMER'))
const isAdmin = computed(() => auth.hasRole('ADMIN_SISTEM', 'ADMIN_DEV'))
const closed = computed(() => ticket.value?.status === 'CLOSED')

async function load() {
  try { ticket.value = await $api<Ticket>(`/api/tickets/${route.params.id}`) } catch (e) { error.value = errText(e) }
}

async function send() {
  if (!reply.value.trim() || busy.value) return
  busy.value = true
  try {
    await $api(`/api/tickets/${route.params.id}/messages`, { method: 'POST', body: { message: reply.value.trim() } })
    reply.value = ''
    await load()
  } catch (e) { toast.error(errText(e)) } finally { busy.value = false }
}

async function setStatus(status: Ticket['status']) {
  confirmClose.value = false
  busy.value = true
  try {
    await $api(`/api/tickets/${route.params.id}/status`, { method: 'PATCH', body: { status, resolution_note: note.value.trim() || undefined } })
    toast.success(`Status diubah ke ${status}.`)
    note.value = ''
    await load()
  } catch (e) { toast.error(errText(e)) } finally { busy.value = false }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <NuxtLink :to="auth.hasRole('ADMIN_SISTEM') ? '/admin/tickets' : auth.hasRole('ADMIN_DEV') ? '/dev' : '/tickets'" class="text-sm text-indigo-700">‹ Kembali</NuxtLink>
    <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
    <p v-else-if="!ticket" class="text-sm text-gray-500">Memuat…</p>
    <template v-else>
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-xl font-bold">{{ ticket.subject }}</h1>
          <p class="text-xs text-gray-500">
            {{ ticket.id }} · {{ ticket.category }} · pemilik {{ ticket.user_id }} · {{ fmtDate(ticket.created_at) }}
            <template v-if="ticket.order_id"> · <NuxtLink :to="`/orders/${ticket.order_id}`" class="text-indigo-700 underline">{{ ticket.order_id }}</NuxtLink></template>
          </p>
        </div>
        <div class="flex gap-2"><StatusBadge kind="priority" :value="ticket.priority" /><StatusBadge kind="ticket" :value="ticket.status" /></div>
      </div>

      <p class="whitespace-pre-wrap rounded-lg border bg-white p-4 text-sm">{{ ticket.description }}</p>
      <p v-if="ticket.resolution_note" class="rounded-md bg-green-50 p-3 text-sm text-green-800"><b>Catatan penyelesaian:</b> {{ ticket.resolution_note }}</p>

      <section class="space-y-2">
        <h2 class="font-semibold">Percakapan</h2>
        <p v-if="!ticket.messages?.length" class="text-sm text-gray-500">Belum ada balasan.</p>
        <div v-for="m in ticket.messages" :key="m.id" class="rounded-lg border p-3 text-sm" :class="m.author_role === 'CUSTOMER' ? 'bg-white' : 'bg-indigo-50'">
          <p class="mb-1 text-xs text-gray-500">{{ m.author_id }} ({{ m.author_role }}) · {{ fmtDate(m.created_at) }}</p>
          <p class="whitespace-pre-wrap">{{ m.message }}</p>
        </div>
      </section>

      <form v-if="!closed" class="space-y-2" @submit.prevent="send">
        <textarea v-model="reply" rows="3" maxlength="2000" class="w-full rounded-md border px-3 py-2 text-sm" placeholder="Tulis balasan…" />
        <button type="submit" class="rounded-md bg-indigo-600 px-4 py-2 text-sm text-white disabled:opacity-50" :disabled="busy || !reply.trim()">Kirim balasan</button>
      </form>
      <p v-else class="text-sm text-gray-500">Tiket ditutup. Buka kembali untuk membalas.</p>

      <section class="flex flex-wrap items-center gap-2 border-t pt-4">
        <template v-if="isCustomer">
          <button v-if="!closed" type="button" class="rounded-md border px-3 py-1.5 text-sm" :disabled="busy" @click="confirmClose = true">Tutup tiket</button>
          <button v-else type="button" class="rounded-md border px-3 py-1.5 text-sm" :disabled="busy" @click="setStatus('OPEN')">Buka kembali</button>
        </template>
        <template v-else-if="isAdmin">
          <input v-model="note" maxlength="500" class="min-w-[220px] flex-1 rounded-md border px-3 py-1.5 text-sm" placeholder="Catatan penyelesaian (opsional)">
          <button v-for="s in (['IN_PROGRESS', 'RESOLVED', 'CLOSED', 'OPEN'] as const)" :key="s" type="button" class="rounded-md border px-3 py-1.5 text-sm disabled:opacity-40" :disabled="busy || ticket.status === s" @click="setStatus(s)">{{ s }}</button>
        </template>
      </section>

      <ConfirmDialog :open="confirmClose" title="Tutup tiket?" message="Anda dapat membukanya kembali nanti." confirm-label="Tutup tiket" @confirm="setStatus('CLOSED')" @cancel="confirmClose = false" />
    </template>
  </div>
</template>
