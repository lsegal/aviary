<template>
  <div v-if="auto?.error" class="mt-1 text-xs" data-testid="auto-update-error">
    <div class="font-medium [overflow-wrap:anywhere]" :class="auto.code === 'git-busy'
      ? 'text-yellow-900 dark:text-yellow-100'
      : 'text-red-700 dark:text-red-300'" :title="auto.code ? auto.error : undefined">{{ message }}</div>
    <div class="mt-1 flex gap-3">
      <button v-if="auto.code === 'git-stale-lock'" type="button" class="font-semibold underline disabled:opacity-60"
        :disabled="disabled" data-testid="auto-update-remove-lock" @click="$emit('removeLock')">Remove stale lock and update</button>
      <button v-else type="button" class="font-semibold underline disabled:opacity-60" :disabled="disabled"
        data-testid="auto-update-retry" @click="$emit('retry')">Retry</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { AutoUpdateStatus } from "../composables/useServerStatus";

// Why the last automatic update failed. Lock contention in a dev checkout
// reads as a short deferral instead of git's raw error (#1049).
const props = defineProps<{ auto?: AutoUpdateStatus; disabled?: boolean }>();
defineEmits<{ retry: []; removeLock: [] }>();

const message = computed(() => {
	const auto = props.auto;
	switch (auto?.code) {
		case "git-busy":
			return "Update deferred: another git operation is running, retrying.";
		case "git-stale-lock":
			return `Update blocked: ${auto.error}. No git command seems to be using it.`;
		default:
			return `Auto update failed: ${auto?.error}`;
	}
});
</script>
