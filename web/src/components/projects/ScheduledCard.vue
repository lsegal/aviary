<template>
	<!-- The whole box opens the task's page, the same one its sidebar row
		opens: its name is a link stretched over the box, and the output and
		the footer's controls sit above it. -->
	<article data-testid="scheduled-card"
		class="relative flex h-full max-h-[50vh] min-w-0 cursor-pointer flex-col overflow-hidden rounded-xl border border-violet-200 bg-white focus-within:ring-2 focus-within:ring-blue-500 hover:border-violet-400 hover:shadow-sm dark:border-violet-900 dark:bg-gray-900 dark:hover:border-violet-700">
		<div class="shrink-0 space-y-1 px-4 pb-2.5 pt-3">
			<div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
				<!-- One card per schedule: a run shows Running here in place, and
					a failed last run shows Failed until the next success or a
					dismissal. -->
				<span v-if="running" data-testid="scheduled-state" data-state="running"
					class="inline-flex items-center gap-1.5 rounded-full bg-blue-50 px-2 py-0.5 font-medium text-blue-700 dark:bg-blue-950/50 dark:text-blue-300">
					<span aria-hidden="true" data-testid="scheduled-spinner"
						class="h-3 w-3 animate-spin rounded-full border-2 border-blue-300 border-t-blue-600 dark:border-blue-800 dark:border-t-blue-300" />
					Running
				</span>
				<span v-else data-testid="scheduled-state" data-state="scheduled"
					class="rounded-full bg-violet-50 px-2 py-0.5 font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">
					Scheduled
				</span>
				<PausedChip v-if="next.kind === 'paused'" />
				<span v-if="failed" data-testid="scheduled-failed"
					class="rounded-full bg-red-50 px-2 py-0.5 font-medium text-red-700 dark:bg-red-950/50 dark:text-red-300">
					Failed
				</span>
				<span class="ml-auto truncate font-mono text-gray-500 dark:text-gray-400">{{ schedule.id }}</span>
			</div>
			<h3 class="text-base font-semibold leading-snug text-gray-900 dark:text-white">
				<router-link :to="taskTo" data-testid="scheduled-card-link"
					class="after:absolute after:inset-0 after:rounded-xl hover:underline focus:outline-none">{{ schedule.name }}</router-link>
			</h3>
			<p class="truncate text-xs text-gray-500 dark:text-gray-400" :title="schedule.prompt">{{ truncatePrompt(schedule.prompt, 120) }}</p>
			<!-- Trigger and next run share one line. A one-time trigger that has
				fired has no next run while its run is in flight: the running
				control below says so, instead of "none". -->
			<p data-testid="scheduled-run-details" class="flex min-w-0 items-center gap-1.5 text-xs text-gray-700 dark:text-gray-200">
				<span data-testid="scheduled-trigger" class="truncate" :title="trigger">{{ trigger }}</span>
				<template v-if="next.kind !== 'running'">
					<span aria-hidden="true" class="text-gray-300 dark:text-gray-600">·</span>
					<span data-testid="scheduled-next" class="shrink-0 whitespace-nowrap"
						:class="next.kind === 'paused' ? 'italic text-gray-400 dark:text-gray-500' : ''"
						:title="next.kind === 'next' ? formatFire(next.at) : ''">
						<template v-if="next.kind === 'paused'">Next run: paused</template>
						<template v-else-if="next.kind === 'queued'">Queued</template>
						<template v-else-if="next.kind === 'next'">Next run {{ nextRunText(next.at, now) }}</template>
						<template v-else>No next run</template>
					</span>
				</template>
			</p>
			<!-- The last finished run: its result and when it ended, with a
				strip of recent results; the output area below shows its
				summary. It stays while the next run is in flight. -->
			<p v-if="last" data-testid="scheduled-last-run" class="flex min-w-0 items-center gap-1.5 text-xs text-gray-700 dark:text-gray-200">
				<span v-if="last.result" data-testid="scheduled-last-result" :data-result="last.result" class="shrink-0 font-semibold"
					:class="runResultClass(last.result)" :title="runResultLabel(last.result)">{{ runResultIcon(last.result) }}</span>
				<component :is="lastTo ? 'router-link' : 'span'" :to="lastTo" data-testid="scheduled-last-time" class="truncate"
					:class="lastTo ? 'relative z-10 hover:underline' : ''" :title="formatFire(last.at)">Last run {{ age(last.at, now) }} ago · {{ (runResultLabel(last.result) || last.status).toLowerCase() }}</component>
				<span v-if="strip.length > 1" data-testid="scheduled-strip" aria-label="Recent results"
					class="ml-auto shrink-0 font-mono tracking-tighter">
					<span v-for="(r, i) in strip" :key="i" :class="runResultClass(r)">{{ runResultIcon(r) }}</span>
				</span>
			</p>
		</div>
		<!-- The output area a job card has: the run's live output while it
			runs, or what the schedule last did or does next between runs. -->
		<JobOutputPane v-if="running && job" :job="job" testid="scheduled-output" class="relative z-10" />
		<div v-else data-testid="scheduled-empty"
			class="flex min-h-0 flex-[1_1_7rem] items-center justify-center bg-slate-950 px-4 py-3 text-center text-xs text-slate-300">
			<p data-testid="scheduled-last" class="flex min-w-0 items-center justify-center gap-1.5">
				<template v-if="running">Waiting for output…</template>
				<template v-else-if="last">
					<span class="shrink-0 font-semibold" :class="runResultClass(last.result)">{{ runResultIcon(last.result) }}</span>
					<span data-testid="scheduled-last-summary" class="truncate" :title="last.summary">{{ last.summary || runResultLabel(last.result) || last.status }}</span>
				</template>
				<template v-else>
					Hasn't run yet<template v-if="next.kind === 'next'"> · next run {{ until(next.at, now) }}</template>
				</template>
			</p>
		</div>
		<div class="relative z-10 flex shrink-0 cursor-auto flex-wrap items-center gap-2 border-t border-gray-100 bg-white px-3 py-2.5 dark:border-gray-800 dark:bg-gray-900">
			<router-link :to="to" aria-label="Edit schedule" title="Edit this schedule"
				class="shrink-0 rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
				<PencilIcon aria-hidden="true" class="h-4 w-4" />
			</router-link>
			<!-- The running state shows once: as the control that opens the run. -->
			<router-link v-if="running" :to="runTo" data-testid="scheduled-running"
				:title="runOpen ? 'Open this run' : 'Open this task'"
				class="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-blue-200 bg-blue-50 px-2 py-1 text-xs font-medium text-blue-700 hover:bg-blue-100 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300 dark:hover:bg-blue-950">
				<span aria-hidden="true" class="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
				{{ activeLabel }}<template v-if="runStarted"> · <span data-testid="scheduled-elapsed">{{ age(runStarted, now) }}</span></template>
				<ArrowsPointingOutIcon aria-hidden="true" class="h-3.5 w-3.5" />
			</router-link>
			<button v-if="failed && job" type="button" :disabled="busy" data-testid="scheduled-dismiss"
				title="Dismiss this failed run; the card still shows it until the next run"
				class="shrink-0 rounded-lg border border-red-200 px-2 py-1 text-xs font-medium text-red-700 hover:bg-red-50 disabled:opacity-50 dark:border-red-900 dark:text-red-300 dark:hover:bg-red-950/40"
				@click="emit('dismiss', job)">
				Dismiss
			</button>
			<button type="button" :disabled="busy" aria-label="Run now" title="Run this schedule now"
				class="shrink-0 rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
				@click="emit('run', schedule)">
				<PlayIcon aria-hidden="true" class="h-4 w-4" />
			</button>
			<button type="button" aria-label="Settings" title="Open this scheduled task's settings"
				class="shrink-0 rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
				@click="emit('settings', schedule)">
				<Cog6ToothIcon aria-hidden="true" class="h-4 w-4" />
			</button>
			<button type="button" :disabled="busy" :aria-label="schedule.enabled ? 'Pause' : 'Resume'"
				:title="schedule.enabled ? 'Pause this schedule' : 'Resume this schedule'"
				class="shrink-0 rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
				@click="emit('toggle', schedule)">
				<PauseIcon v-if="schedule.enabled" aria-hidden="true" class="h-4 w-4" />
				<PlayCircleIcon v-else aria-hidden="true" class="h-4 w-4" />
			</button>
			<JobOutputToggle v-if="running && job" />
			<div class="flex min-w-0 flex-1 flex-col items-end text-right font-mono text-xs">
				<span v-if="agentLabel" data-testid="scheduled-agent" class="max-w-full truncate text-gray-500 dark:text-gray-400"
					:title="agentLabel">{{ agentLabel }}</span>
			</div>
		</div>
	</article>
</template>

<script setup lang="ts">
import ArrowsPointingOutIcon from "@heroicons/vue/24/outline/ArrowsPointingOutIcon";
import Cog6ToothIcon from "@heroicons/vue/24/outline/Cog6ToothIcon";
import PauseIcon from "@heroicons/vue/24/outline/PauseIcon";
import PencilIcon from "@heroicons/vue/24/outline/PencilIcon";
import PlayCircleIcon from "@heroicons/vue/24/outline/PlayCircleIcon";
import PlayIcon from "@heroicons/vue/24/outline/PlayIcon";
import { computed } from "vue";
import type { RouteLocationRaw } from "vue-router";
import { projectOwner } from "../../lib/projectSettings";
import {
	activeScheduleRun,
	age,
	formatFire,
	jobPath,
	nextRunText,
	type ProjectJob,
	type ProjectSchedule,
	type RunResult,
	runResultClass,
	runResultIcon,
	runResultLabel,
	scheduleLastRun,
	scheduleNext,
	scheduleRunning,
	triggerSummary,
	truncatePrompt,
	until,
} from "../../lib/projects";
import { taskSessionPath, useSettingsStore } from "../../stores/settings";
import PausedChip from "../tasks/PausedChip.vue";
import JobOutputPane from "./JobOutputPane.vue";
import JobOutputToggle from "./JobOutputToggle.vue";

// A scheduled task's board box, built like a job card so the two share a
// grid row's height: header, output area and action footer.
const props = defineProps<{
	schedule: ProjectSchedule;
	now: number;
	/** Where the box's Edit schedule link opens the schedule's editor. */
	to: RouteLocationRaw;
	/**
	 * The schedule's board entry, its latest run with its last and next run
	 * and recent runs, whose output the box shows while it runs.
	 */
	job?: ProjectJob;
	busy?: boolean;
}>();
const emit = defineEmits<{
	run: [schedule: ProjectSchedule];
	settings: [schedule: ProjectSchedule];
	toggle: [schedule: ProjectSchedule];
	dismiss: [job: ProjectJob];
}>();

const entry = computed(() => props.job?.schedule);
const trigger = computed(() => triggerSummary(props.schedule));
const next = computed(() => scheduleNext(props.schedule, entry.value));
const active = computed(() => activeScheduleRun(props.schedule));
const last = computed(() => scheduleLastRun(props.schedule, entry.value));
// A run in flight shows on this card in place, never as a card of its own.
const running = computed(
	() => scheduleRunning(entry.value) || (!entry.value && !!active.value),
);
const failed = computed(() => entry.value?.state === "failed");
// The run's view opens while the board has the running run's job.
const runOpen = computed(() => scheduleRunning(entry.value));
const runStarted = computed(() =>
	runOpen.value ? props.job?.startedAt : active.value?.fireTime,
);
// The most recent finished runs' results, oldest first.
const strip = computed<RunResult[]>(() =>
	(entry.value?.recentRuns ?? [])
		.filter((r) => r.result)
		.slice(0, 10)
		.map((r) => r.result as RunResult)
		.reverse(),
);
// The agent that runs the task and its model override, where a job card
// names its skill and CLI.
const agentLabel = computed(() =>
	[props.schedule.agent, props.schedule.model].filter(Boolean).join(" · "),
);
const activeLabel = computed(() => {
	if (entry.value?.state === "starting") return "Starting";
	if (runOpen.value) return "Running now";
	switch (active.value?.status) {
		case "queued":
			return "Queued";
		case "retrying":
			return "Retrying";
		default:
			return "Running now";
	}
});

// The task's page: its session, which every run shares.
const taskTo = computed(() =>
	taskSessionPath(props.schedule.agent, props.schedule.name),
);

const settings = useSettingsStore();
// The last run's line opens that run's view when the board has it.
const lastTo = computed(() => {
	const job = props.job;
	const number = last.value?.number;
	if (!job || !number) return undefined;
	return jobPath(projectOwner(settings.config, job.target), job.target, number);
});
// The running control opens the running run's view; without it, the task's
// page, whose session shows the run.
const runTo = computed<RouteLocationRaw>(() => {
	const job = props.job;
	if (!job || !runOpen.value) return taskTo.value;
	return jobPath(
		projectOwner(settings.config, job.target),
		job.target,
		job.number,
	);
});
</script>
