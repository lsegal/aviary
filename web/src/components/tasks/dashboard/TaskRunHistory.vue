<template>
	<section aria-label="Run history" data-testid="task-run-history" class="space-y-2">
		<div class="flex flex-wrap items-center gap-2">
			<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Run history</h3>
			<div role="group" aria-label="Filter runs by result" class="flex flex-wrap items-center gap-1.5 text-xs">
				<button v-for="f in FILTERS" :key="f" type="button" :aria-pressed="filter === f" :data-testid="`task-run-filter-${f}`"
					class="shrink-0 rounded-full border px-2.5 py-0.5"
					:class="filter === f ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-300 text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-800'"
					@click="filter = f">
					{{ f === "all" ? "All" : RESULT_LABELS[f] }}<span class="ml-1 tabular-nums opacity-75">{{ countOf(f) }}</span>
				</button>
			</div>
		</div>

		<p v-if="entries.length === 0" data-testid="task-run-history-empty"
			class="rounded-xl border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
			No runs yet. Runs appear here once the task fires or you run it now.
		</p>
		<p v-else-if="filtered.length === 0" data-testid="task-run-history-empty"
			class="rounded-xl border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
			No {{ RESULT_LABELS[filter as TaskRunResult].toLowerCase() }} runs.
		</p>
		<ul v-else class="divide-y divide-gray-200 overflow-hidden rounded-xl border border-gray-200 dark:divide-gray-800 dark:border-gray-800">
			<li v-for="e in visible" :key="e.id" data-testid="task-run-row" :data-result="e.result" class="px-3 py-2 text-sm">
				<div class="flex flex-wrap items-center gap-x-3 gap-y-1">
					<TaskRunResultPill :result="e.result" />
					<span class="text-gray-900 dark:text-white" :title="e.startedAt">{{ new Date(e.startedAt).toLocaleString() }}</span>
					<span class="tabular-nums text-gray-500 dark:text-gray-400" data-testid="task-run-duration">{{ runDurationText(e, now) }}</span>
					<span class="text-gray-500 dark:text-gray-400" data-testid="task-run-trigger">{{ triggerLabel(e.trigger) }}</span>
					<router-link v-if="e.job" :to="taskRunPath(agent, task, e.id)" data-testid="task-run-open"
						class="ml-auto text-xs font-medium text-blue-600 hover:underline dark:text-blue-400">
						Open run
					</router-link>
				</div>
				<div v-if="e.error" class="mt-1 flex items-start gap-2 text-xs">
					<p data-testid="task-run-error"
						class="line-clamp-2 min-w-0 flex-1 whitespace-pre-wrap wrap-anywhere text-red-600 dark:text-red-400" :title="e.error">
						{{ e.error }}
					</p>
					<a v-if="e.bugReportUrl" :href="e.bugReportUrl" target="_blank" rel="noopener noreferrer"
						data-testid="task-run-report-bug"
						class="shrink-0 font-medium text-blue-600 hover:underline dark:text-blue-400">Report bug</a>
				</div>
				<p v-if="e.skippedReply !== undefined" data-testid="task-run-skipped-reply"
					class="mt-1 line-clamp-2 text-xs text-gray-500 dark:text-gray-400" :title="e.skippedReply">
					Skipped reply: {{ e.skippedReply }}
				</p>
			</li>
		</ul>
		<button v-if="filtered.length > shown" type="button" data-testid="task-run-history-more"
			class="w-full rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
			@click="shown += PAGE_SIZE">
			Show {{ Math.min(PAGE_SIZE, filtered.length - shown) }} more of {{ filtered.length - shown }}
		</button>
	</section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
	RESULT_LABELS,
	runDurationText,
	type TaskRunEntry,
	type TaskRunResult,
	triggerLabel,
} from "../../../lib/taskRuns";
import { taskRunPath } from "../../../stores/settings";
import TaskRunResultPill from "./TaskRunResultPill.vue";

// A task's previous runs, newest first, a page at a time, filtered by result.

export type TaskRunFilter = TaskRunResult | "all";

const PAGE_SIZE = 20;
const FILTERS: TaskRunFilter[] = [
	"all",
	"succeeded",
	"fallback",
	"failed",
	"canceled",
	"skipped",
	"running",
	"queued",
];

const props = defineProps<{
	entries: TaskRunEntry[];
	agent: string;
	task: string;
	now: number;
}>();
const filter = defineModel<TaskRunFilter>("filter", { default: "all" });

const filtered = computed(() =>
	filter.value === "all"
		? props.entries
		: props.entries.filter((e) => e.result === filter.value),
);
const shown = ref(PAGE_SIZE);
const visible = computed(() => filtered.value.slice(0, shown.value));
watch(filter, () => {
	shown.value = PAGE_SIZE;
});

function countOf(f: TaskRunFilter): number {
	return f === "all"
		? props.entries.length
		: props.entries.filter((e) => e.result === f).length;
}
</script>
