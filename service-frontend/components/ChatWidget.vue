<script setup lang="ts">
// Floating / Embedded AI Chat (TRD 07 §4.2). Teks agent dirender TANPA v-html: tokenizer aman (tebal + tautan ID).
const props = withDefaults(defineProps<{ mode?: 'floating' | 'embedded'; initialPrompt?: string }>(), { mode: 'floating' })

const chat = useChat()
const auth = useAuth()
const input = ref('')
const listEl = ref<HTMLElement | null>(null)

const suggestions = computed(() =>
  auth.isAuthenticated.value
    ? ['Status pesanan saya', 'Kenapa pesanan ORD-003 gagal?', 'Berapa lama garansi?', 'Buat tiket komplain']
    : ['Cari powerbank di bawah 400 ribu', 'Mouse yang tidak bikin pergelangan pegal', 'Syarat retur barang?'],
)

type Seg = { t: 'text' | 'bold' | 'link'; v: string; to?: string }
const TOKEN = /(\*\*[^*\n]+\*\*|\b(?:ORD|TKT|PRD|KB)-\d{3,}\b)/g
const ROUTES: Record<string, (id: string) => string> = {
  ORD: (id) => `/orders/${id}`,
  TKT: (id) => `/tickets/${id}`,
  PRD: (id) => `/products/${id}`,
  KB: (id) => `/help?doc=${id}`,
}

function segments(line: string): Seg[] {
  return line.split(TOKEN).filter(Boolean).map((p): Seg => {
    if (p.startsWith('**') && p.endsWith('**')) return { t: 'bold', v: p.slice(2, -2) }
    const m = /^(ORD|TKT|PRD|KB)-\d{3,}$/.exec(p)
    if (m) return { t: 'link', v: p, to: ROUTES[m[1]!]!(p) }
    return { t: 'text', v: p }
  })
}

async function submit(text?: string) {
  const t = (text ?? input.value).slice(0, 1000)
  input.value = ''
  await chat.send(t)
}

watch([() => chat.messages.value.length, () => chat.loading.value], async () => {
  await nextTick()
  listEl.value?.scrollTo({ top: listEl.value.scrollHeight })
})

onMounted(() => { if (props.initialPrompt && chat.messages.value.length === 0) void submit(props.initialPrompt) })
</script>

<template>
  <div>
    <button
      v-if="mode === 'floating' && !chat.open.value"
      type="button"
      class="fixed bottom-5 right-5 z-40 h-14 w-14 rounded-full bg-indigo-600 text-2xl text-white shadow-lg hover:bg-indigo-700"
      aria-label="Buka Asisten Toko AI"
      @click="chat.open.value = true"
    >💬</button>

    <section
      v-if="mode === 'embedded' || chat.open.value"
      :class="mode === 'floating'
        ? 'fixed bottom-5 right-5 z-40 flex h-[560px] w-[380px] max-w-[calc(100vw-2rem)] flex-col rounded-xl border bg-white shadow-2xl max-sm:inset-0 max-sm:h-auto max-sm:w-auto max-sm:max-w-none max-sm:rounded-none'
        : 'flex h-[560px] w-full flex-col rounded-xl border bg-white'"
      aria-label="Asisten Toko AI"
    >
      <header class="flex items-center justify-between rounded-t-xl bg-indigo-600 px-4 py-3 text-white">
        <div>
          <p class="font-semibold">Asisten Toko AI</p>
          <p class="text-xs opacity-80">{{ auth.user.value ? `${auth.user.value.name} · ${auth.user.value.role}` : 'Tamu' }}</p>
        </div>
        <div class="flex items-center gap-2 text-sm">
          <button type="button" class="rounded px-2 py-1 hover:bg-indigo-500" @click="chat.reset()">Baru</button>
          <button v-if="mode === 'floating'" type="button" class="rounded px-2 py-1 hover:bg-indigo-500" aria-label="Tutup" @click="chat.open.value = false">✕</button>
        </div>
      </header>

      <div ref="listEl" class="flex-1 space-y-3 overflow-y-auto p-4">
        <div v-if="chat.messages.value.length === 0" class="space-y-2 text-sm text-gray-600">
          <p>Halo! Tanyakan produk, pesanan, atau kebijakan toko.</p>
          <button v-for="s in suggestions" :key="s" type="button" class="mr-2 mb-2 rounded-full border px-3 py-1 text-xs hover:bg-gray-50" @click="submit(s)">{{ s }}</button>
        </div>

        <div v-for="m in chat.messages.value" :key="m.id" :class="m.from === 'user' ? 'text-right' : 'text-left'">
          <div
            class="inline-block max-w-[85%] rounded-lg px-3 py-2 text-left text-sm"
            :class="m.from === 'user' ? 'bg-indigo-600 text-white' : 'bg-gray-100 text-gray-900'"
          >
            <p v-for="(line, i) in m.text.split('\n')" :key="i" class="min-h-[1em] whitespace-pre-wrap">
              <template v-for="(s, j) in segments(line)" :key="j">
                <strong v-if="s.t === 'bold'">{{ s.v }}</strong>
                <NuxtLink v-else-if="s.t === 'link' && m.from === 'agent'" :to="s.to!" class="font-medium text-indigo-700 underline">{{ s.v }}</NuxtLink>
                <template v-else>{{ s.v }}</template>
              </template>
            </p>
          </div>
        </div>

        <p v-if="chat.loading.value" class="text-sm text-gray-500">Asisten sedang mengetik…</p>
        <p v-if="chat.error.value" class="text-sm text-red-600">{{ chat.error.value }}</p>
      </div>

      <form class="flex gap-2 border-t p-3" @submit.prevent="submit()">
        <textarea
          v-model="input"
          rows="1"
          maxlength="1000"
          class="flex-1 resize-none rounded-md border px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
          placeholder="Tulis pesan…"
          @keydown.enter.exact.prevent="submit()"
        />
        <button type="submit" class="rounded-md bg-indigo-600 px-4 text-sm text-white disabled:opacity-50" :disabled="chat.loading.value || !input.trim()">Kirim</button>
      </form>
    </section>
  </div>
</template>
