<template>
	<section data-testid="kanban" aria-label="Jobs by state" class="flex gap-3 overflow-x-auto pb-2">
		<div v-if="showScheduled && scheduleTo" data-testid="kanban-column" data-state="scheduled"
			class="flex min-w-44 flex-1 basis-0 flex-col rounded-xl border border-violet-200 bg-violet-50/40 dark:border-violet-900 dark:bg-violet-950/20">
			<h3 class="flex items-center gap-2 border-b border-violet-200 px-3 py-2 text-xs font-semibold uppercase tracking-wide text-violet-700 dark:border-violet-900 dark:text-violet-300">
				<span class="h-2 w-2 rounded-full bg-violet-500" />
				Scheduled
				<span class="ml-auto font-normal tabular-nums text-violet-400">{{ schedules.length }}</span>
			</h3>
			<div class="flex flex-col gap-2 p-2">
				<article v-for="s in schedules" :key="s.id" data-testid="kanban-scheduled-card"
					class="min-w-0 space-y-1 rounded-lg border border-violet-200 bg-white px-3 py-2 text-xs dark:border-violet-900 dark:bg-gray-900">
					<router-link :to="scheduleTo(s)" :title="s.id"
						class="block truncate text-sm font-medium text-gray-900 hover:underline dark:text-white">
						{{ s.name }}
					</router-link>
					<p data-testid="kanban-scheduled-next" class="tabular-nums text-violet-700 dark:text-violet-300"
						:title="s.nextRun ? formatFire(s.nextRun) : ''">
						<template v-if="scheduleNext(s).kind === 'paused'">Paused</template>
						<template v-else-if="s.nextRun">Next {{ until(s.nextRun, now) }}</template>
						<template v-else-if="scheduleNext(s).kind === 'running'">Running now</template>
						<template v-else>No next run</template>
					</p>
					<p data-testid="kanban-scheduled-prompt" class="truncate text-gray-500 dark:text-gray-400" :title="s.prompt">
						{{ truncatePrompt(s.prompt, 60) }}
					</p>
				</article>
				<p v-if="!schedules.length" class="px-1 py-3 text-center text-gray-400">No schedules</p>
			</div>
		</div>
		<div v-for="col in columns" :key="col.state" data-testid="kanban-column" :data-state="col.state"
			class="flex min-w-44 flex-1 basis-0 flex-col rounded-xl border border-gray-200 bg-gray-50 dark:border-gray-800 dark:bg-gray-900/60">
			<h3 class="flex items-center gap-2 border-b border-gray-200 px-3 py-2 text-xs font-semibold uppercase tracking-wide text-gray-600 dark:border-gray-800 dark:text-gray-300">
				<span class="h-2 w-2 rounded-full" :class="DOTS[col.state]" />
				{{ stateLabel(col.state) }}
				<span class="ml-auto font-normal tabular-nums text-gray-400">{{ col.jobs.length }}</span>
			</h3>
			<div class="flex flex-col gap-2 p-2">
				<article v-for="job in col.jobs" :key="job.id" data-testid="kanban-card"
					class="min-w-0 space-y-1 rounded-lg border border-gray-200 bg-white px-3 py-2 text-xs dark:border-gray-700 dark:bg-gray-900">
					<router-link :to="runPath(job)" :title="job.title || job.id"
						class="block truncate text-sm font-medium text-gray-900 hover:underline dark:text-white">
						{{ job.title || job.id }}
					</router-link>
					<p class="flex flex-wrap items-center gap-x-1.5 text-gray-500 dark:text-gray-400">
						<span v-if="job.scheduled">Scheduled</span>
						<a v-else-if="job.number && job.repo" :href="job.issueURL || githubURL(job.repo, 'issues', job.number)"
							target="_blank" rel="noreferrer" class="hover:underline">#{{ job.number }}</a>
						<PullRequestLink v-if="job.pr" :job="job" />
						<span v-if="job.startedAt" data-testid="kanban-timing" class="ml-auto tabular-nums"
							:title="`Started ${new Date(job.startedAt).toLocaleString()}`">{{ age(job.startedAt, now) }}</span>
					</p>
					<p v-if="job.state === 'paused'" data-testid="kanban-paused-reason" class="text-amber-700 dark:text-amber-400">
						{{ waitText(job, now) || "No pause reason recorded" }}
					</p>
				</article>
				<p v-if="!col.jobs.length" class="px-1 py-3 text-center text-gray-400">No jobs</p>
			</div>
		</div>
	</section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { RouteLocationRaw } from "vue-router";
import { projectOwner } from "../../lib/projectSettings";
import {
	age,
	BOARD_STATES,
	type BoardFilter,
	type BoardState,
	formatFire,
	githubURL,
	jobPath,
	jobStates,
	type ProjectJob,
	type ProjectSchedule,
	scheduleNext,
	stateLabel,
	truncatePrompt,
	until,
	waitText,
} from "../../lib/projects";
import { useSettingsStore } from "../../stores/settings";
import PullRequestLink from "./PullRequestLink.vue";

const props = withDefaults(
	defineProps<{
		jobs: ProjectJob[];
		/** The selected filters; none shows every column. */
		filters: BoardFilter[];
		now: number;
		/** The project's scheduled tasks, shown in their own Scheduled column. */
		schedules?: ProjectSchedule[];
		/** Where a scheduled box opens its detail view; without it there is no Scheduled column. */
		scheduleTo?: (s: ProjectSchedule) => RouteLocationRaw;
	}>(),
	{ schedules: () => [], scheduleTo: undefined },
);

const DOTS: Record<BoardState, string> = {
	starting: "bg-orange-500",
	paused: "bg-amber-500",
	running: "bg-blue-500",
	done: "bg-green-500",
	failed: "bg-red-500",
};

// Selected filters narrow the board to their columns.
const showScheduled = computed(
	() => !props.filters.length || props.filters.includes("scheduled"),
);
const columns = computed(() =>
	(props.filters.length ? jobStates(props.filters) : BOARD_STATES).map(
		(state) => ({
			state,
			jobs: props.jobs.filter((j) => j.state === state),
		}),
	),
);

const settings = useSettingsStore();
function runPath(job: ProjectJob) {
	return jobPath(
		projectOwner(settings.config, job.target),
		job.target,
		job.number,
	);
}
</script>
