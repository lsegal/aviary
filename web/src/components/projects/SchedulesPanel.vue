<template>
	<div class="grid grid-cols-[minmax(0,1fr)] gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
		<section aria-label="Schedules" class="space-y-2">
			<div class="flex items-center gap-2">
				<h2 class="text-sm font-semibold text-gray-800 dark:text-gray-100">Project scheduled tasks</h2>
				<button type="button" data-testid="schedule-new"
					class="ml-auto rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
					:disabled="!agents.length" @click="onNew">New schedule</button>
			</div>
			<p v-if="error || toggleError" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
				{{ toggleError || error }}
			</p>
			<p v-if="loaded && !schedules.length" class="py-8 text-center text-sm text-gray-400">
				No scheduled tasks target this project yet.
			</p>
			<ul class="space-y-2">
				<li v-for="s in schedules" :key="s.id" data-testid="schedule-row"
					class="flex items-start gap-3 rounded-xl border bg-white px-3 py-2.5 dark:bg-gray-900"
					:class="selected === s.id ? 'border-blue-500 ring-1 ring-blue-500' : 'border-gray-200 dark:border-gray-800'">
					<button type="button" role="switch" :aria-checked="s.enabled" :aria-label="`Enable ${s.name}`"
						:disabled="toggling === s.id"
						class="relative mt-0.5 inline-flex h-5 w-9 shrink-0 rounded-full transition-colors disabled:opacity-50"
						:class="s.enabled ? 'bg-blue-600' : 'bg-gray-300 dark:bg-gray-700'"
						@click="onToggle(s)">
						<span class="absolute top-0.5 h-4 w-4 rounded-full bg-white shadow transition-all"
							:class="s.enabled ? 'left-4.5' : 'left-0.5'" />
					</button>
					<button type="button" class="min-w-0 flex-1 text-left" @click="selected = s.id">
						<p class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ s.name }}</p>
						<p class="truncate text-xs text-gray-500 dark:text-gray-400">
							{{ triggerSummary(s) }}<template v-if="s.enabled && s.nextRun"> · next {{ until(s.nextRun, now) }}</template>
							<template v-else-if="!s.enabled"> · paused</template>
						</p>
						<p v-if="lastScheduleRun(s)" data-testid="schedule-last-result"
							class="mt-0.5 flex items-center gap-1.5 truncate text-xs text-gray-500 dark:text-gray-400">
							<span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="scheduleRunDot(lastScheduleRun(s)?.status ?? '')" />
							<span class="truncate">{{ lastScheduleRun(s)?.summary || lastScheduleRun(s)?.status }}</span>
							<span class="shrink-0 text-gray-400">· {{ age(lastScheduleRun(s)?.fireTime ?? "", now) }} ago</span>
							<span v-if="s.history[0]?.status === 'running'" class="shrink-0 text-blue-600 dark:text-blue-400">· running now</span>
						</p>
					</button>
				</li>
			</ul>
		</section>
		<section aria-label="Schedule editor"
			class="rounded-xl border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-900">
			<ScheduleEditor v-if="editing" :key="editing.id" :schedule="editing" :save="save"
				:remove="remove" :run-now="runNow" :preview="preview" @saved="selected = $event" @deleted="selected = ''" />
			<p v-else class="py-12 text-center text-sm text-gray-400">Select a schedule to edit it, or create a new one.</p>
		</section>
	</div>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { useAgentItemDialogs } from "../../composables/useAgentItemDialogs";
import {
	age,
	lastScheduleRun,
	type ProjectSchedule,
	scheduleRunDot,
	triggerSummary,
	until,
} from "../../lib/projects";
import type { TriggerFields } from "../../lib/trigger";
import ScheduleEditor from "./ScheduleEditor.vue";

const props = defineProps<{
	project: string;
	schedules: ProjectSchedule[];
	agents: string[];
	error: string;
	loaded: boolean;
	now: number;
	save: (s: ProjectSchedule) => Promise<void>;
	setEnabled: (s: ProjectSchedule, enabled: boolean) => Promise<void>;
	remove: (s: ProjectSchedule) => Promise<void>;
	runNow: (s: ProjectSchedule) => Promise<void>;
	preview: (s: TriggerFields) => Promise<string[]>;
	refresh: () => Promise<void>;
}>();

/** The schedule id the editor shows, settable from the "Up next" strip. */
const selected = defineModel<string>("selected", { default: "" });

const editing = computed(
	() => props.schedules.find((s) => s.id === selected.value) ?? null,
);

// A new schedule is created the way any scheduled task is, through the shared
// Add task dialog, as a "project" task targeting this project.
const { open: openItemDialog } = useAgentItemDialogs();
function onNew() {
	const [agent] = props.agents;
	if (!agent) return;
	openItemDialog({
		kind: "task",
		mode: "create",
		agent,
		agents: props.agents,
		project: props.project,
		onCreated: (owner, name) => {
			selected.value = `${owner}/${name}`;
			void props.refresh();
		},
	});
}

const toggling = ref("");
const toggleError = ref("");
async function onToggle(s: ProjectSchedule) {
	toggling.value = s.id;
	try {
		await props.setEnabled(s, !s.enabled);
		toggleError.value = "";
	} catch (e) {
		toggleError.value = e instanceof Error ? e.message : String(e);
	} finally {
		toggling.value = "";
	}
}
</script>
