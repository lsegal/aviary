<template>
	<span class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-semibold" :class="toneClass">
		<span class="h-1.5 w-1.5 rounded-full" :class="[dotClass, status === 'running' ? 'animate-pulse' : '']" />
		{{ statusLabel(status) }}
	</span>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { type JobStatus, statusLabel, statusTone } from "../../lib/projects";

const props = defineProps<{ status: JobStatus }>();

const TONES: Record<string, [string, string]> = {
	blue: [
		"bg-blue-50 text-blue-700 dark:bg-blue-950/50 dark:text-blue-300",
		"bg-blue-500",
	],
	amber: [
		"bg-amber-50 text-amber-800 dark:bg-amber-950/50 dark:text-amber-300",
		"bg-amber-500",
	],
	red: [
		"bg-red-50 text-red-700 dark:bg-red-950/50 dark:text-red-300",
		"bg-red-500",
	],
	green: [
		"bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300",
		"bg-green-500",
	],
	gray: [
		"bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300",
		"bg-gray-400",
	],
};

const toneClass = computed(() => TONES[statusTone(props.status)][0]);
const dotClass = computed(() => TONES[statusTone(props.status)][1]);
</script>
