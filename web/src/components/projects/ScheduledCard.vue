<template>
	<!-- The whole box opens the task's page, the same one its sidebar row
		opens: its name is a link stretched over the box, and the controls sit
		above it. The pencil opens the schedule's editor. -->
	<article data-testid="scheduled-card" :class="{ 'self-start': !job }"
		class="relative flex max-h-[50vh] min-w-0 flex-col overflow-hidden rounded-xl border border-violet-200 bg-white focus-within:ring-2 focus-within:ring-blue-500 hover:border-violet-400 hover:shadow-sm dark:border-violet-900 dark:bg-gray-900 dark:hover:border-violet-700">
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
						:title="next.kind === 'next' ? formatFire(next.at) : ''">
						<template v-if="next.kind === 'paused'">paused</template>
						<template v-else-if="next.kind === 'next'">next {{ until(next.at, now) }}</template>
						<template v-else>no next run</template>
					</span>
				</template>
			</p>
		</div>
		<JobOutputPane v-if="job" :job="job" testid="scheduled-output" class="relative z-10" />
		<div class="flex shrink-0 items-center gap-2 border-t border-gray-100 px-3 py-2 dark:border-gray-800">
			<router-link :to="to" aria-label="Edit schedule" title="Edit this schedule"
				class="relative z-10 shrink-0 rounded-lg border border-gray-200 bg-white p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200 dark:hover:bg-gray-800">
				<PencilIcon aria-hidden="true" class="h-4 w-4" />
			</router-link>
			<!-- The running state shows once: as the control that opens the run. -->
			<router-link v-if="active" :to="runTo" data-testid="scheduled-running"
				:title="job ? 'Open this run' : 'Open this task'"
				class="relative z-10 inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-blue-200 bg-blue-50 px-2 py-1 text-xs font-medium text-blue-700 hover:bg-blue-100 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-300 dark:hover:bg-blue-950">
				<span aria-hidden="true" class="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" />
				{{ activeLabel }} · {{ age(active.fireTime, now) }}
				<ArrowsPointingOutIcon aria-hidden="true" class="h-3.5 w-3.5" />
			</router-link>
			<span v-if="last || !schedule.history.length" data-testid="scheduled-last"
				class="flex min-w-0 flex-1 items-center gap-1.5 text-xs text-gray-700 dark:text-gray-200">
				<template v-if="last">
					<span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="scheduleRunDot(last.status)" />
					<span class="truncate" :title="last.summary">{{ last.summary || last.status }}</span>
					<span class="shrink-0 text-gray-400">· {{ age(last.fireTime, now) }} ago</span>
				</template>
				<template v-else>never</template>
			</span>
			<span v-else class="flex-1" />
			<JobOutputToggle v-if="job" class="relative z-10" />
		</div>
	</article>
</template>

<script setup lang="ts">
import ArrowsPointingOutIcon from "@heroicons/vue/24/outline/ArrowsPointingOutIcon";
import PencilIcon from "@heroicons/vue/24/outline/PencilIcon";
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
import { taskSessionPath, useSettingsStore } from "../../stores/settings";
import JobOutputPane from "./JobOutputPane.vue";
import JobOutputToggle from "./JobOutputToggle.vue";

// Without output to fill it, the box keeps its own height (self-start)
// instead of stretching to its grid row's tallest card.
const props = defineProps<{
	schedule: ProjectSchedule;
	now: number;
	/** Where the box's pencil opens the schedule's editor. */
	to: RouteLocationRaw;
	/** The board job of the schedule's latest run, whose output the box shows. */
	job?: ProjectJob;
}>();

const trigger = computed(() => triggerSummary(props.schedule));
const next = computed(() => scheduleNext(props.schedule));
const active = computed(() => activeScheduleRun(props.schedule));
const last = computed(() => lastScheduleRun(props.schedule));
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

// The task's page: its session, which every run shares.
const taskTo = computed(() =>
	taskSessionPath(props.schedule.agent, props.schedule.name),
);

const settings = useSettingsStore();
// The running control opens the run's view when the board has its job; an
// earlier run's finished job is not the one running, so it opens the task's
// page, whose session shows the run.
const runTo = computed<RouteLocationRaw>(() => {
	const job = props.job;
	if (!job || job.state === "done" || job.state === "failed")
		return taskTo.value;
	return jobPath(
		projectOwner(settings.config, job.target),
		job.target,
		job.number,
	);
});
</script>
