<template>
	<article data-testid="scheduled-card"
		class="flex h-full max-h-[50vh] min-w-0 flex-col overflow-hidden rounded-xl border border-violet-200 bg-white dark:border-violet-900 dark:bg-gray-900">
		<div class="shrink-0 space-y-1 px-4 pb-2.5 pt-3">
			<div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
				<span class="rounded-full bg-violet-50 px-2 py-0.5 font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">
					Scheduled
				</span>
				<span v-if="!schedule.enabled" class="rounded-full bg-gray-100 px-2 py-0.5 font-medium text-gray-600 dark:bg-gray-800 dark:text-gray-300">
					Paused
				</span>
				<span class="ml-auto truncate font-mono text-gray-500 dark:text-gray-400">{{ schedule.id }}</span>
			</div>
			<h3 class="text-base font-semibold leading-snug text-gray-900 dark:text-white">
				<router-link :to="to" class="hover:underline">{{ schedule.name }}</router-link>
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
						:title="next.kind === 'next' ? formatFire(next.at) : ''">
						<template v-if="next.kind === 'paused'">paused</template>
						<template v-else-if="next.kind === 'next'">next {{ until(next.at, now) }}</template>
						<template v-else>no next run</template>
					</span>
				</template>
			</p>
		</div>
		<!-- The output area a job card has: the latest run's output, or what
			the schedule last did or does next when the board has no run. -->
		<JobOutputPane v-if="job" :job="job" testid="scheduled-output" />
		<div v-else data-testid="scheduled-empty"
			class="flex min-h-0 flex-[1_1_7rem] items-center justify-center bg-slate-950 px-4 py-3 text-center text-xs text-slate-300">
			<p data-testid="scheduled-last" class="flex min-w-0 items-center justify-center gap-1.5">
				<template v-if="last">
					<span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="scheduleRunDot(last.status)" />
					<span class="truncate" :title="last.summary">{{ last.summary || last.status }}</span>
					<span class="shrink-0 text-slate-500">· {{ age(last.fireTime, now) }} ago</span>
				</template>
				<template v-else-if="!active">
					Hasn't run yet<template v-if="next.kind === 'next'"> · next run {{ until(next.at, now) }}</template>
				</template>
				<template v-else>Waiting for output…</template>
			</p>
		</div>
		<div class="flex shrink-0 flex-wrap items-center gap-2 border-t border-gray-100 px-3 py-2.5 dark:border-gray-800">
			<!-- The running state shows once: as the control that opens the run. -->
			<router-link v-if="active" :to="runTo" data-testid="scheduled-running"
				:title="job ? 'Open this run' : 'Open this schedule'"
				class="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-blue-200 bg-blue-50 px-2 py-1 text-xs font-medium text-blue-700 hover:bg-blue-100 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300 dark:hover:bg-blue-950">
				<span aria-hidden="true" class="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
				{{ activeLabel }} · {{ age(active.fireTime, now) }}
				<ArrowsPointingOutIcon aria-hidden="true" class="h-3.5 w-3.5" />
			</router-link>
			<router-link v-else :to="to" aria-label="Edit schedule" title="Edit this schedule"
				class="shrink-0 rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
				<ArrowsPointingOutIcon aria-hidden="true" class="h-4 w-4" />
			</router-link>
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
			<JobOutputToggle v-if="job" />
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
	lastScheduleRun,
	type ProjectJob,
	type ProjectSchedule,
	scheduleNext,
	scheduleRunDot,
	triggerSummary,
	truncatePrompt,
	until,
} from "../../lib/projects";
import { useSettingsStore } from "../../stores/settings";
import JobOutputPane from "./JobOutputPane.vue";
import JobOutputToggle from "./JobOutputToggle.vue";

// A scheduled task's board box, built like a job card so the two share a
// grid row's height: header, output area and action footer.
const props = defineProps<{
	schedule: ProjectSchedule;
	now: number;
	/** Where the box opens its detail view. */
	to: RouteLocationRaw;
	/** The board job of the schedule's latest run, whose output the box shows. */
	job?: ProjectJob;
	busy?: boolean;
}>();
const emit = defineEmits<{
	run: [schedule: ProjectSchedule];
	settings: [schedule: ProjectSchedule];
	toggle: [schedule: ProjectSchedule];
}>();

const trigger = computed(() => triggerSummary(props.schedule));
const next = computed(() => scheduleNext(props.schedule));
const active = computed(() => activeScheduleRun(props.schedule));
const last = computed(() => lastScheduleRun(props.schedule));
// The agent that runs the task and its model override, where a job card
// names its skill and CLI.
const agentLabel = computed(() =>
	[props.schedule.agent, props.schedule.model].filter(Boolean).join(" · "),
);
const activeLabel = computed(() => {
	switch (active.value?.status) {
		case "queued":
			return "Queued";
		case "retrying":
			return "Retrying";
		default:
			return "Running now";
	}
});

const settings = useSettingsStore();
// The running control opens the run's view when the board has its job; an
// earlier run's finished job is not the one running.
const runTo = computed<RouteLocationRaw>(() => {
	const job = props.job;
	if (!job || job.state === "done" || job.state === "failed") return props.to;
	return jobPath(
		projectOwner(settings.config, job.target),
		job.target,
		job.number,
	);
});
</script>
