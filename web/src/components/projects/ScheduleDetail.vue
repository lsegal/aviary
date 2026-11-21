<template>
	<section data-testid="schedule-detail" :aria-label="`Schedule ${schedule.name}`"
		class="space-y-4 rounded-xl border border-violet-200 bg-white p-4 dark:border-violet-900 dark:bg-gray-900">
		<header class="flex flex-wrap items-center gap-2">
			<router-link :to="back" aria-label="Back to board" title="Back to board"
				class="rounded-lg border border-gray-200 p-1.5 text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800">
				<ArrowLeftIcon aria-hidden="true" class="h-4 w-4" />
			</router-link>
			<span class="rounded-full bg-violet-50 px-2 py-0.5 text-xs font-medium text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">Scheduled</span>
			<h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ schedule.name }}</h2>
			<span class="rounded-full px-2 py-0.5 text-xs font-medium"
				:class="schedule.enabled ? 'bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300' : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'">
				{{ schedule.enabled ? "Enabled" : "Paused" }}
			</span>
			<div class="ml-auto flex items-center gap-2">
				<button type="button" :disabled="busy"
					class="rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
					@click="onRunNow">Run now</button>
				<router-link :to="edit"
					class="rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-500">Edit</router-link>
			</div>
		</header>

		<p v-if="error" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
			{{ error }}
		</p>

		<div class="grid gap-4 lg:grid-cols-2">
			<section aria-label="Schedule timings" data-testid="schedule-timings" class="space-y-1 text-sm">
				<h3 class="font-medium text-gray-700 dark:text-gray-200">Schedule</h3>
				<dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
					<template v-for="row in timings" :key="row.label">
						<dt class="text-gray-500 dark:text-gray-400">{{ row.label }}</dt>
						<dd class="break-words font-mono text-gray-800 dark:text-gray-100">{{ row.value }}</dd>
					</template>
				</dl>
			</section>
			<section aria-label="Next predicted runs" class="space-y-1 text-sm">
				<h3 class="font-medium text-gray-700 dark:text-gray-200">Next predicted runs</h3>
				<p v-if="!schedule.enabled" class="text-xs text-gray-400">Paused; it fires again once enabled.</p>
				<p v-else-if="predictError" class="text-xs text-red-600 dark:text-red-400">{{ predictError }}</p>
				<ol v-else-if="predicted.length" data-testid="schedule-predicted" class="space-y-0.5 font-mono text-xs text-gray-700 dark:text-gray-200">
					<li v-for="t in predicted" :key="t" class="flex gap-2">
						<span>{{ formatFire(t) }}</span>
						<span class="text-gray-400">{{ until(t, now) }}</span>
					</li>
				</ol>
				<p v-else class="text-xs text-gray-400">No upcoming runs.</p>
			</section>
		</div>

		<section aria-label="Prompt" class="space-y-1 text-sm">
			<h3 class="font-medium text-gray-700 dark:text-gray-200">Prompt</h3>
			<pre data-testid="schedule-prompt"
				class="whitespace-pre-wrap break-words rounded-lg bg-gray-50 px-3 py-2 font-mono text-xs text-gray-800 dark:bg-gray-950 dark:text-gray-100">{{ schedule.prompt }}</pre>
		</section>

		<section aria-label="Settings" data-testid="schedule-settings" class="space-y-1 text-sm">
			<h3 class="font-medium text-gray-700 dark:text-gray-200">Settings</h3>
			<dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
				<template v-for="row in settingRows" :key="row.label">
					<dt class="text-gray-500 dark:text-gray-400">{{ row.label }}</dt>
					<dd class="break-words text-gray-800 dark:text-gray-100">{{ row.value }}</dd>
				</template>
			</dl>
		</section>

		<section aria-label="Last runs" class="space-y-1 text-sm">
			<h3 class="font-medium text-gray-700 dark:text-gray-200">Last runs</h3>
			<ul v-if="schedule.history.length" data-testid="schedule-last-runs"
				class="divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-gray-800 dark:border-gray-800">
				<li v-for="run in schedule.history" :key="run.fireTime" class="flex items-center gap-2 px-3 py-1.5 text-xs">
					<span class="h-2 w-2 shrink-0 rounded-full" :class="scheduleRunDot(run.status)" />
					<span class="w-16 shrink-0 text-gray-500">{{ run.status }}</span>
					<span class="min-w-0 flex-1 truncate text-gray-700 dark:text-gray-200" :title="run.summary">{{ run.summary || run.reason }}</span>
					<span class="shrink-0 tabular-nums text-gray-400">{{ runDuration(run.durationMS) }}</span>
					<span class="shrink-0 text-gray-400">{{ formatFire(run.fireTime) }}</span>
				</li>
			</ul>
			<p v-else class="text-xs text-gray-400">It hasn't run yet.</p>
		</section>
	</section>
</template>

<script setup lang="ts">
import ArrowLeftIcon from "@heroicons/vue/24/outline/ArrowLeftIcon";
import { computed, ref, watch } from "vue";
import type { RouteLocationRaw } from "vue-router";
import {
	formatFire,
	type ProjectSchedule,
	runDuration,
	scheduleRunDot,
	triggerSummary,
	until,
} from "../../lib/projects";

const props = defineProps<{
	schedule: ProjectSchedule;
	now: number;
	back: RouteLocationRaw;
	edit: RouteLocationRaw;
	predict: (s: ProjectSchedule) => Promise<string[]>;
	runNow: (s: ProjectSchedule) => Promise<void>;
}>();

const TRIGGER_LABELS: Record<ProjectSchedule["trigger"], string> = {
	once: "Once",
	interval: "Interval",
	daily: "Daily",
	weekly: "Weekly",
	cron: "Cron",
};

/** The trigger definition: only the fields its trigger type uses. */
const timings = computed(() => {
	const s = props.schedule;
	const rows = [
		{ label: "Summary", value: triggerSummary(s) },
		{ label: "Trigger", value: TRIGGER_LABELS[s.trigger] },
	];
	if (s.trigger === "cron") rows.push({ label: "Cron", value: s.schedule });
	if (s.trigger === "interval") rows.push({ label: "Every", value: s.every });
	if (s.trigger === "weekly") rows.push({ label: "Days", value: s.days });
	if (s.trigger === "daily" || s.trigger === "weekly")
		rows.push({ label: "At", value: s.at });
	if (s.startAt) rows.push({ label: "Starts", value: formatFire(s.startAt) });
	rows.push({ label: "Time zone", value: s.timezone || "Local" });
	return rows;
});

const settingRows = computed(() => {
	const s = props.schedule;
	return [
		{ label: "Agent", value: s.agent },
		{ label: "Model", value: s.model || "the project agent's model" },
		{
			label: "Overlap",
			value: s.overlap === "skip" ? "skip a fire while running" : "queue",
		},
		{
			label: "Missed runs",
			value: s.missed === "run_once" ? "run once to catch up" : "skip",
		},
	];
});

const predicted = ref<string[]>([]);
const predictError = ref("");
// The scheduler's predictions move whenever the schedule fires or changes.
watch(
	() => [props.schedule.id, props.schedule.enabled, props.schedule.nextRun],
	async () => {
		if (!props.schedule.enabled) return;
		try {
			predicted.value = await props.predict(props.schedule);
			predictError.value = "";
		} catch (e) {
			predicted.value = [];
			predictError.value = e instanceof Error ? e.message : String(e);
		}
	},
	{ immediate: true },
);

const busy = ref(false);
const error = ref("");
async function onRunNow() {
	busy.value = true;
	try {
		await props.runNow(props.schedule);
		error.value = "";
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		busy.value = false;
	}
}
</script>
