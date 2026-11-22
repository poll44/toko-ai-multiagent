<script setup lang="ts">
const { toasts } = useToast()
const cls = { success: 'bg-green-600', error: 'bg-red-600', info: 'bg-gray-800' } as const
</script>
<template>
  <div class="pointer-events-none fixed right-4 top-16 z-[60] flex w-80 flex-col gap-2" aria-live="polite">
    <div v-for="t in toasts" :key="t.id" class="pointer-events-auto rounded-md px-4 py-2 text-sm text-white shadow-lg" :class="cls[t.kind]">{{ t.text }}</div>
  </div>
</template>
