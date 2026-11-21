<template>
  <button type="button" data-testid="connection-status" :data-state="status" :aria-label="label" :title="title"
    class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg transition hover:bg-gray-200 dark:hover:bg-gray-800"
    :class="[colorClass, status === 'disconnected' ? '' : 'cursor-default']" @click="onClick">
    <WifiIcon v-if="status === 'connected'" aria-hidden="true" class="h-4 w-4" />
    <SignalSlashIcon v-else-if="status === 'disconnected'" aria-hidden="true" class="h-4 w-4" />
    <svg v-else viewBox="0 0 24 24" fill="none" aria-hidden="true" class="h-4 w-4 animate-spin">
      <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="2.5" class="opacity-25" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
    </svg>
  </button>
</template>

<script setup lang="ts">
// The server connection as one icon: its shape, color, tooltip and label all
// name the state, so it never relies on color alone. Clicking it while
// disconnected retries the connection.
import SignalSlashIcon from "@heroicons/vue/24/outline/SignalSlashIcon";
import WifiIcon from "@heroicons/vue/24/outline/WifiIcon";
import { computed } from "vue";
import type { ConnectionStatus } from "../composables/useServerStatus";

const props = defineProps<{ status: ConnectionStatus }>();
const emit = defineEmits<{ retry: [] }>();

const label = computed(() => {
	if (props.status === "connected") return "Connected";
	if (props.status === "disconnected") return "Disconnected";
	return "Connecting…";
});
const title = computed(() =>
	props.status === "disconnected"
		? "Disconnected — click to retry"
		: label.value,
);
const colorClass = computed(() => {
	if (props.status === "connected") return "text-green-700 dark:text-green-400";
	if (props.status === "disconnected") return "text-red-600 dark:text-red-400";
	return "text-orange-600 dark:text-orange-400";
});

function onClick() {
	if (props.status === "disconnected") emit("retry");
}
</script>
