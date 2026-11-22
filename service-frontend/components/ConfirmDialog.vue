<script setup lang="ts">
defineProps<{ open: boolean; title: string; message?: string; confirmLabel?: string; danger?: boolean }>()
const emit = defineEmits<{ confirm: []; cancel: [] }>()
</script>
<template>
  <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" role="dialog" aria-modal="true" @keydown.esc="emit('cancel')">
    <div class="w-full max-w-sm rounded-lg bg-white p-5 shadow-xl">
      <h3 class="font-semibold">{{ title }}</h3>
      <p v-if="message" class="mt-2 text-sm text-gray-600">{{ message }}</p>
      <slot />
      <div class="mt-4 flex justify-end gap-2">
        <button type="button" class="rounded-md border px-3 py-1.5 text-sm" @click="emit('cancel')">Batal</button>
        <button type="button" class="rounded-md px-3 py-1.5 text-sm text-white" :class="danger ? 'bg-red-600' : 'bg-indigo-600'" @click="emit('confirm')">{{ confirmLabel ?? 'Ya' }}</button>
      </div>
    </div>
  </div>
</template>
