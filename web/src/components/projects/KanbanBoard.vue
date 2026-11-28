<template>
	<section data-testid="kanban" aria-label="Jobs by state" class="flex gap-3 overflow-x-auto pb-2">
		<div v-if="showScheduled" data-testid="kanban-column" data-state="scheduled"
			class="flex min-w-44 flex-1 basis-0 flex-col rounded-xl border border-violet-200 bg-violet-50/40 dark:border-violet-900 dark:bg-violet-950/20">
			<h3 class="flex items-center gap-2 border-b border-violet-200 px-3 py-2 text-xs font-semibold uppercase tracking-wide text-violet-700 dark:border-violet-900 dark:text-violet-300">
				<span class="h-2 w-2 rounded-full bg-violet-500" />
				Scheduled
				<span class="ml-auto font-normal tabular-nums text-violet-400">{{ schedules.length }}</span>
			</h3>
			<div class="flex flex-col gap-2 p-2">
				<!-- The card opens the task's page; the gear, its task settings. -->
				<article v-for="s in schedules" :key="s.id" data-testid="kanban-scheduled-card"
					class="relative min-w-0 cursor-pointer space-y-1 rounded-lg border border-violet-200 bg-white px-3 py-2 text-xs focus-within:ring-2 focus-within:ring-blue-500 hover:border-violet-400 dark:border-violet-900 dark:bg-gray-900 dark:hover:border-violet-700">
					<div class="flex items-center gap-1">
						<router-link :to="taskSessionPath(s.agent, s.name)" :title="s.id"
							class="min-w-0 flex-1 truncate text-sm font-medium text-gray-900 after:absolute after:inset-0 after:rounded-lg hover:underline focus:outline-none dark:text-white">
							{{ s.name }}
						</router-link>
						<!-- A run shows here in place, on the name's line, so the card
							never moves columns or changes height (#1157). -->
						<span v-if="scheduleRunning(entryOf(s)?.schedule)" data-testid="kanban-scheduled-running"
							:title="entryOf(s)?.startedAt ? `Running since ${formatFire(entryOf(s)!.startedAt)}` : 'Running'"
							class="inline-flex shrink-0 items-center gap-1 tabular-nums text-blue-700 dark:text-blue-300">
							<span aria-hidden="true" class="h-2.5 w-2.5 animate-spin rounded-full border-2 border-blue-300 border-t-blue-600" />
							<span class="sr-only">Running</span><template v-if="entryOf(s)?.startedAt">{{ age(entryOf(s)!.startedAt, now) }}</template>
						</span>
						<button type="button" data-testid="kanban-scheduled-settings" aria-label="Task settings" title="Task settings"
							class="relative z-10 shrink-0 rounded p-0.5 text-gray-500 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
							@click="emit('settings', s)">
							<Cog6ToothIcon aria-hidden="true" class="h-3.5 w-3.5" />
						</button>
					</div>
					<ProjectChip v-if="showProject" :project="s.project" compact data-testid="kanban-scheduled-project"
						class="relative z-10 block w-fit" />
					<p data-testid="kanban-scheduled-next" class="tabular-nums text-violet-700 dark:text-violet-300"
						:title="nextOf(s).kind === 'next' ? formatFire(s.nextRun) : ''">
						<PausedChip v-if="nextOf(s).kind === 'paused'" />
						<template v-else-if="nextOf(s).kind === 'queued'">Queued</template>
						<template v-else-if="s.nextRun">Next {{ until(s.nextRun, now) }}</template>
						<template v-else-if="nextOf(s).kind === 'running'">Running now</template>
						<template v-else>No next run</template>
					</p>
					<p v-if="lastOf(s)" data-testid="kanban-scheduled-last" class="flex items-center gap-1 text-gray-500 dark:text-gray-400">
						<span v-if="lastOf(s)!.result" :class="runResultClass(lastOf(s)!.result)">{{ runResultIcon(lastOf(s)!.result) }}</span>
						Last run {{ age(lastOf(s)!.at, now) }} ago
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
				<article v-for="job in col.shown" :key="job.id" data-testid="kanban-card"
					class="min-w-0 space-y-1 rounded-lg border border-gray-200 bg-white px-3 py-2 text-xs dark:border-gray-700 dark:bg-gray-900">
					<router-link :to="runPath(job)" :title="job.title || job.id"
						class="block truncate text-sm font-medium text-gray-900 hover:underline dark:text-white">
						{{ job.title || job.id }}
					</router-link>
					<p class="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-gray-500 dark:text-gray-400">
						<ProjectChip v-if="showProject" :project="job.target" compact data-testid="kanban-project" />
						<span v-if="job.scheduled">Scheduled</span>
						<a v-else-if="job.kind === 'merge' || job.kind === 'discuss'" :href="job.issueURL" target="_blank" rel="noreferrer"
							data-testid="kanban-kind" class="hover:underline">{{ job.kind === "merge" ? "Safe merge PR" : "Discuss" }} #{{ job.number }}</a>
						<a v-else-if="job.number && job.repo" :href="job.issueURL || githubURL(job.repo, 'issues', job.number)"
							target="_blank" rel="noreferrer" class="hover:underline">#{{ job.number }}</a>
						<PullRequestLink v-if="job.pr && job.kind !== 'merge'" :job="job" />
						<span v-if="job.kind === 'merge' && job.status === 'queued' && job.queuePosition" data-testid="kanban-queue-position">queued · priority #{{ job.queuePosition }}</span>
						<span v-if="job.startedAt" data-testid="kanban-timing" class="ml-auto tabular-nums"
							:title="`Started ${new Date(job.startedAt).toLocaleString()}`">{{ age(job.startedAt, now) }}</span>
					</p>
					<p v-if="job.state === 'paused'" data-testid="kanban-paused-reason" class="text-amber-700 dark:text-amber-400">
						{{ waitText(job, now) || "No pause reason recorded" }}
					</p>
					<p v-if="(job.kind === 'merge' || job.kind === 'discuss') && job.summary && (job.state === 'done' || job.state === 'failed')"
						data-testid="kanban-reason" class="truncate" :title="job.summary"
						:class="job.state === 'failed' ? 'text-red-600 dark:text-red-400' : 'text-gray-500 dark:text-gray-400'">
						{{ job.summary }}
					</p>
				</article>
				<!-- A long column, such as Done on a busy project, renders a page of
					cards at a time rather than hundreds at once (#1657). -->
				<button v-if="col.jobs.length > col.shown.length" type="button" data-testid="kanban-show-more"
					class="rounded-lg border border-dashed border-gray-300 px-3 py-1.5 text-xs text-gray-600 hover:bg-white dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-900"
					@click="showMore(col.state)">
					Show {{ Math.min(KANBAN_COLUMN_PAGE, col.jobs.length - col.shown.length) }} more
					<span class="text-gray-400">({{ col.jobs.length - col.shown.length }} hidden)</span>
				</button>
				<p v-if="!col.jobs.length" class="px-1 py-3 text-center text-gray-400">No jobs</p>
			</div>
		</div>
	</section>
</template>

<script setup lang="ts">
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import { computed, reactive } from "vue";
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
	runResultClass,
	runResultIcon,
	scheduleJob,
	scheduleLastRun,
	scheduleNext,
	scheduleRunning,
	stateLabel,
	truncatePrompt,
	until,
	waitText,
} from "../../lib/projects";
import { taskSessionPath, useSettingsStore } from "../../stores/settings";
import PausedChip from "../tasks/PausedChip.vue";
import ProjectChip from "./ProjectChip.vue";
import PullRequestLink from "./PullRequestLink.vue";

const props = withDefaults(
	defineProps<{
		jobs: ProjectJob[];
		/** The selected filters; none shows every column. */
		filters: BoardFilter[];
		now: number;
		/** The project's scheduled tasks, shown in their own Scheduled column. */
		schedules?: ProjectSchedule[];
		/** The schedules' board entries: their last and next runs and any run in flight. */
		scheduleEntries?: ProjectJob[];
		/** Names each card's project, on a board of several. */
		showProject?: boolean;
	}>(),
	{
		schedules: () => [],
		scheduleEntries: () => [],
		showProject: false,
	},
);
const emit = defineEmits<{ settings: [schedule: ProjectSchedule] }>();

function entryOf(s: ProjectSchedule) {
	return scheduleJob(props.scheduleEntries, s.id);
}
function nextOf(s: ProjectSchedule) {
	return scheduleNext(s, entryOf(s)?.schedule);
}
function lastOf(s: ProjectSchedule) {
	return scheduleLastRun(s, entryOf(s)?.schedule);
}

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
// Cards each column renders before its "Show more" button.
const KANBAN_COLUMN_PAGE = 50;
const shownCounts = reactive<Partial<Record<BoardState, number>>>({});
function showMore(state: BoardState) {
	shownCounts[state] =
		(shownCounts[state] ?? KANBAN_COLUMN_PAGE) + KANBAN_COLUMN_PAGE;
}
const columns = computed(() =>
	(props.filters.length ? jobStates(props.filters) : BOARD_STATES).map(
		(state) => {
			const jobs = props.jobs.filter((j) => j.state === state);
			return {
				state,
				jobs,
				shown: jobs.slice(0, shownCounts[state] ?? KANBAN_COLUMN_PAGE),
			};
		},
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
