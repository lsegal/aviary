<template>
	<span data-testid="task-run-result" :data-result="result"
		class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-semibold" :class="TONES[result][0]">
		<span class="h-1.5 w-1.5 rounded-full" :class="[TONES[result][1], result === 'running' ? 'animate-pulse' : '']" />
		{{ RESULT_LABELS[result] }}
	</span>
</template>

<script setup lang="ts">
import { RESULT_LABELS, type TaskRunResult } from "../../../lib/taskRuns";

defineProps<{ result: TaskRunResult }>();

// Each result's pill and dot colors, matching the Code Loop job pills.
const TONES: Record<TaskRunResult, [string, string]> = {
	queued: [
		"bg-violet-50 text-violet-700 dark:bg-violet-950/50 dark:text-violet-300",
		"bg-violet-500",
	],
	running: [
		"bg-blue-50 text-blue-700 dark:bg-blue-950/50 dark:text-blue-300",
		"bg-blue-500",
	],
	succeeded: [
		"bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300",
		"bg-green-500",
	],
	failed: [
		"bg-red-50 text-red-700 dark:bg-red-950/50 dark:text-red-300",
		"bg-red-500",
	],
	canceled: [
		"bg-amber-50 text-amber-800 dark:bg-amber-950/50 dark:text-amber-300",
		"bg-amber-500",
	],
	skipped: [
		"bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
		"bg-gray-400",
	],
};
</script>
