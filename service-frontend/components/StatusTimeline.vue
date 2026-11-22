<script setup lang="ts">
defineProps<{ history: { from_status: string | null; to_status: string; note: string | null; actor_id: string; created_at: string }[] }>()
</script>
<template>
  <ol class="space-y-3 border-l-2 border-indigo-200 pl-4">
    <li v-for="(h, i) in history" :key="i" class="relative">
      <span class="absolute -left-[22px] top-1.5 h-3 w-3 rounded-full bg-indigo-500" />
      <StatusBadge kind="order" :value="h.to_status" />
      <span class="ml-2 text-xs text-gray-500">{{ fmtDate(h.created_at) }} · {{ h.actor_id }}</span>
      <p v-if="h.note" class="text-sm text-gray-600">{{ h.note }}</p>
    </li>
  </ol>
</template>
