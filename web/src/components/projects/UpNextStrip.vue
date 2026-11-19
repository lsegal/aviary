<template>
	<section v-if="runs.length" data-testid="up-next" aria-label="Up next"
		class="flex flex-wrap items-center gap-2 rounded-xl border border-violet-200 bg-violet-50/60 px-3 py-2 text-xs dark:border-violet-900 dark:bg-violet-950/30">
		<span class="font-semibold uppercase tracking-wide text-violet-700 dark:text-violet-300">Up next</span>
		<button v-for="run in runs" :key="`${run.id}@${run.at}`" type="button" data-testid="up-next-run"
			class="inline-flex items-center gap-1.5 rounded-full border border-violet-200 bg-white px-2.5 py-1 text-gray-700 hover:border-violet-400 dark:border-violet-800 dark:bg-gray-900 dark:text-gray-200"
			:title="new Date(run.at).toLocaleString()" @click="emit('open', run.id)">
			<b class="font-medium">{{ run.name }}</b>
			<span class="text-gray-500 dark:text-gray-400">{{ until(run.at, now) }}</span>
		</button>
	</section>
</template>

<script setup lang="ts">
import { type UpcomingRun, until } from "../../lib/projects";

defineProps<{ runs: UpcomingRun[]; now: number }>();
const emit = defineEmits<{ open: [id: string] }>();
</script>
