<template>
  <div v-if="restart && (restart.phase !== 'idle' || restart.error)" role="status" data-testid="update-status"
    class="rounded-xl border px-3 py-2 text-sm" :class="restart.error
      ? 'border-red-300 bg-red-50 text-red-900 dark:border-red-800 dark:bg-red-950/40 dark:text-red-100'
      : 'border-blue-300 bg-blue-50 text-blue-900 dark:border-blue-800 dark:bg-blue-950/40 dark:text-blue-100'">
    <template v-if="restart.error">{{ action }} failed: {{ restart.error }}</template>
    <template v-else-if="restart.phase === 'updating'">
      <div class="font-medium">{{ restart.upgrade ? `Updating to ${restart.version}…` : "Restarting…" }}</div>
      <div class="mt-0.5 text-xs opacity-80">Running jobs resume after the restart. This page reconnects on its own.</div>
    </template>
    <template v-else>
      <div class="font-medium">{{ action }} waits for {{ jobCount }}.</div>
      <div class="mt-0.5 text-xs opacity-80">New jobs are held until it's done.</div>
      <button type="button" class="mt-1 text-xs font-semibold underline" @click="$emit('cancel')">Cancel</button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { RestartStatus } from "../composables/useServerStatus";

const props = defineProps<{ restart: RestartStatus | null }>();
defineEmits<{ cancel: [] }>();

const action = computed(() =>
	props.restart?.upgrade ? `Upgrade to ${props.restart.version}` : "Restart",
);
const jobCount = computed(() => {
	const n = props.restart?.jobs.length ?? 0;
	return n === 1 ? "1 running job" : `${n} running jobs`;
});
</script>
