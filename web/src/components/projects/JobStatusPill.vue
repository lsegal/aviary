<template>
	<span data-testid="job-state" :data-state="state"
		class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-semibold" :class="TONES[state][0]">
		<span class="h-1.5 w-1.5 rounded-full" :class="[TONES[state][1], state === 'running' || state === 'starting' ? 'animate-pulse' : '']" />
		{{ stateLabel(state) }}
	</span>
</template>

<script setup lang="ts">
import { type BoardState, stateLabel } from "../../lib/projects";

defineProps<{ state: BoardState }>();

// Each board state's pill and dot colors; Starting is orange.
const TONES: Record<BoardState, [string, string]> = {
	starting: [
		"bg-orange-50 text-orange-700 dark:bg-orange-950/50 dark:text-orange-300",
		"bg-orange-500",
	],
	paused: [
		"bg-amber-50 text-amber-800 dark:bg-amber-950/50 dark:text-amber-300",
		"bg-amber-500",
	],
	running: [
		"bg-blue-50 text-blue-700 dark:bg-blue-950/50 dark:text-blue-300",
		"bg-blue-500",
	],
	done: [
		"bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300",
		"bg-green-500",
	],
	failed: [
		"bg-red-50 text-red-700 dark:bg-red-950/50 dark:text-red-300",
		"bg-red-500",
	],
};
</script>
