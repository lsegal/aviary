<template>
	<section v-if="visible.length" data-testid="up-next" aria-label="Up next"
		class="flex items-center gap-2 overflow-x-auto whitespace-nowrap rounded-xl border border-violet-200 bg-violet-50/60 px-3 py-2 text-xs dark:border-violet-900 dark:bg-violet-950/30">
		<span class="shrink-0 font-semibold uppercase tracking-wide text-violet-700 dark:text-violet-300">Up next</span>
		<button v-for="run in visible" :key="`${run.id}@${run.at}`" type="button" data-testid="up-next-run"
			class="inline-flex shrink-0 items-center gap-1.5 rounded-full border border-violet-200 bg-white px-2.5 py-1 text-gray-700 hover:border-violet-400 dark:border-violet-800 dark:bg-gray-900 dark:text-gray-200"
			:title="showProject && run.project ? `${run.project} · ${new Date(run.at).toLocaleString()}` : new Date(run.at).toLocaleString()" @click="emit('open', run.id)">
			<b class="font-medium">{{ run.name }}</b>
			<span class="text-gray-500 dark:text-gray-400">{{ until(run.at, now) }}</span>
		</button>
	</section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { type UpcomingRun, until, upNextRuns } from "../../lib/projects";

const props = defineProps<{
	runs: UpcomingRun[];
	now: number;
	showProject?: boolean;
}>();
// Only runs due within the window show, re-checked as now ticks, so the
// strip is hidden when nothing is about to run.
const visible = computed(() => upNextRuns(props.runs, props.now));
const emit = defineEmits<{ open: [id: string] }>();
</script>
