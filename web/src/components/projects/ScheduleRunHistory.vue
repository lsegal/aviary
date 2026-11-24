<template>
	<!-- A scheduled task's run history: when each run fired, how long it took,
		how it ended and its summary. A run the board has opens its transcript;
		older runs the scheduler alone remembers are listed without a link. -->
	<section v-if="rows.length" aria-label="Run history" class="text-sm">
		<h3 class="mb-1 font-medium text-gray-700 dark:text-gray-200">Run history</h3>
		<ul data-testid="schedule-runs" class="divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-gray-800 dark:border-gray-800">
			<li v-for="row in rows" :key="row.key" data-testid="schedule-run" :data-result="row.result || row.status">
				<component :is="row.to ? 'router-link' : 'div'" :to="row.to"
					class="flex items-center gap-2 px-3 py-1.5 text-xs"
					:class="row.to ? 'hover:bg-gray-50 dark:hover:bg-gray-800/60' : ''"
					:title="row.to ? 'Open this run\'s transcript' : undefined">
					<span data-testid="schedule-run-result" class="w-4 shrink-0 text-center font-semibold" :class="row.running ? 'text-blue-500' : runResultClass(row.result)"
						:title="row.running ? 'Running' : runResultLabel(row.result) || row.status">
						<span v-if="row.running" aria-hidden="true" class="inline-block h-2 w-2 animate-pulse rounded-full bg-blue-500" />
						<template v-else>{{ runResultIcon(row.result) }}</template>
					</span>
					<span data-testid="schedule-run-time" class="w-36 shrink-0 text-gray-500 dark:text-gray-400" :title="row.at">{{ formatFire(row.at) }}</span>
					<span data-testid="schedule-run-duration" class="w-14 shrink-0 tabular-nums text-gray-400">{{ row.running ? age(row.at, now) : runDuration(row.durationMS) }}</span>
					<span data-testid="schedule-run-summary" class="min-w-0 flex-1 truncate text-gray-700 dark:text-gray-200" :title="row.summary">{{ row.summary || (row.running ? "Running…" : runResultLabel(row.result) || row.status) }}</span>
				</component>
			</li>
		</ul>
	</section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { RouteLocationRaw } from "vue-router";
import { projectOwner } from "../../lib/projectSettings";
import {
	age,
	finishedScheduleRun,
	formatFire,
	jobPath,
	type ProjectJob,
	type ProjectSchedule,
	type RunResult,
	runDuration,
	runResultClass,
	runResultIcon,
	runResultLabel,
} from "../../lib/projects";
import { useSettingsStore } from "../../stores/settings";

const props = defineProps<{
	schedule: ProjectSchedule;
	/** The schedule's board entry, whose recent runs open their transcripts. */
	entry?: ProjectJob;
	now: number;
}>();

interface Row {
	key: string;
	at: string;
	durationMS: number;
	result: RunResult | "";
	status: string;
	summary: string;
	running: boolean;
	to?: RouteLocationRaw;
}

const settings = useSettingsStore();
const rows = computed<Row[]>(() => {
	const entry = props.entry;
	const runs = entry?.schedule?.recentRuns ?? [];
	if (entry && runs.length) {
		const owner = projectOwner(settings.config, entry.target);
		return runs.map((run) => ({
			key: String(run.number),
			at: run.startedAt,
			durationMS: run.durationMS,
			result: run.result,
			status: run.status,
			summary: run.summary,
			running: !run.result,
			to: jobPath(owner, entry.target, run.number),
		}));
	}
	return props.schedule.history.map((run) => {
		const last = finishedScheduleRun(run);
		return {
			key: run.fireTime,
			at: run.fireTime,
			durationMS: run.durationMS,
			result: last?.result ?? "",
			status: run.status,
			summary: last?.summary ?? "",
			running: !last,
		};
	});
});
</script>
