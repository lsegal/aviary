<template>
	<form data-testid="schedule-editor" class="space-y-4" @submit.prevent="onSave">
		<div class="grid gap-3 sm:grid-cols-2">
			<label class="block text-sm">
				<span class="font-medium text-gray-700 dark:text-gray-200">Name</span>
				<input v-model="draft.name" name="name" required :disabled="!!draft.id" :class="INPUT" placeholder="nightly-triage" />
			</label>
			<label class="block text-sm">
				<span class="font-medium text-gray-700 dark:text-gray-200">Owner agent</span>
				<select v-model="draft.agent" name="agent" required :disabled="!!draft.id" :class="INPUT">
					<option v-for="a in agentOptions" :key="a" :value="a">{{ a }}</option>
				</select>
			</label>
		</div>
		<label class="block text-sm">
			<span class="font-medium text-gray-700 dark:text-gray-200">Prompt</span>
			<textarea v-model="draft.prompt" name="prompt" required rows="5" :class="INPUT"
				placeholder="Triage issues opened since the last run and label them." />
			<span class="text-xs text-gray-500">Runs in an isolated clone of {{ draft.project }} in place of /gh-fix.</span>
		</label>
		<div data-testid="schedule-model" class="block text-sm">
			<span class="font-medium text-gray-700 dark:text-gray-200">Model override (optional)</span>
			<div class="mt-1">
				<ModelSelector :model-value="draft.model" :options="availableModelOptions" placeholder="Default project agent model"
					@update:model-value="draft.model = typeof $event === 'string' ? $event : ''" />
			</div>
			<span class="text-xs text-gray-500">The project's agent runs this task with this model instead of its own. Leave empty to use the agent's model.</span>
		</div>

		<fieldset class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-gray-800">
			<legend class="px-1 text-sm font-medium text-gray-700 dark:text-gray-200">Trigger</legend>
			<div role="radiogroup" class="flex flex-wrap overflow-hidden rounded-lg border border-gray-200 text-sm dark:border-gray-700">
				<button v-for="t in TRIGGERS" :key="t.key" type="button" role="radio" :aria-checked="draft.trigger === t.key"
					class="flex-1 px-3 py-1.5" :class="draft.trigger === t.key ? 'bg-blue-600 text-white' : 'text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'"
					@click="draft.trigger = t.key">{{ t.label }}</button>
			</div>
			<div class="grid gap-3 sm:grid-cols-2">
				<label v-if="draft.trigger === 'once'" class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">Start at</span>
					<input v-model="startAtLocal" name="start_at" type="datetime-local" required :class="INPUT" />
				</label>
				<label v-if="draft.trigger === 'interval'" class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">Every</span>
					<input v-model="draft.every" name="every" required :class="INPUT" placeholder="30m, 2h, 1d" />
				</label>
				<label v-if="draft.trigger === 'daily' || draft.trigger === 'weekly'" class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">At</span>
					<input v-model="draft.at" name="at" required :class="INPUT" placeholder="09:00 or 09:00,17:30" />
				</label>
				<div v-if="draft.trigger === 'weekly'" class="text-sm sm:col-span-2">
					<span class="text-gray-600 dark:text-gray-300">Days</span>
					<div class="mt-1 flex flex-wrap gap-1">
						<button v-for="d in DAYS" :key="d" type="button" :aria-pressed="days.includes(d)"
							class="rounded-md border px-2 py-1 text-xs capitalize"
							:class="days.includes(d) ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-200 text-gray-600 dark:border-gray-700 dark:text-gray-300'"
							@click="toggleDay(d)">{{ d }}</button>
					</div>
				</div>
				<label v-if="draft.trigger === 'cron'" class="block text-sm sm:col-span-2">
					<span class="text-gray-600 dark:text-gray-300">Cron expression</span>
					<input v-model="draft.schedule" name="schedule" required :class="INPUT" class="font-mono" placeholder="0 9 * * 1-5" />
				</label>
				<label class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">Timezone</span>
					<input v-model="draft.timezone" name="timezone" list="schedule-timezones" :class="INPUT" :placeholder="localZone" />
					<datalist id="schedule-timezones">
						<option v-for="z in zones" :key="z" :value="z" />
					</datalist>
				</label>
			</div>
			<div class="grid gap-3 sm:grid-cols-2">
				<label class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">If the last run is still going</span>
					<select v-model="draft.overlap" name="overlap" :class="INPUT">
						<option value="queue">Queue this run</option>
						<option value="skip">Skip this run</option>
					</select>
				</label>
				<label class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">Runs missed while asleep or stopped</span>
					<select v-model="draft.missed" name="missed" :class="INPUT">
						<option value="skip">Skip them</option>
						<option value="run_once">Run once to catch up</option>
					</select>
				</label>
			</div>
			<div data-testid="schedule-preview" class="text-xs">
				<p class="font-medium text-gray-600 dark:text-gray-300">Next runs</p>
				<p v-if="previewError" class="text-red-600 dark:text-red-400">{{ previewError }}</p>
				<ol v-else-if="nextRuns.length" class="mt-1 space-y-0.5 font-mono text-gray-700 dark:text-gray-200">
					<li v-for="t in nextRuns" :key="t">{{ formatFire(t) }}</li>
				</ol>
				<p v-else class="text-gray-400">No upcoming runs.</p>
			</div>
		</fieldset>

		<label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
			<input v-model="draft.enabled" type="checkbox" name="enabled" class="rounded" />
			Enabled
		</label>

		<p v-if="error" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
			{{ error }}
		</p>

		<div class="flex flex-wrap items-center gap-2">
			<button type="submit" :disabled="busy"
				class="rounded-lg bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50">Save</button>
			<button v-if="draft.id" type="button" :disabled="busy"
				class="rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
				@click="act(() => props.runNow(schedule))">Run now</button>
			<button v-if="draft.id" type="button" :disabled="busy"
				class="ml-auto rounded-lg border border-red-200 px-3 py-1.5 text-sm text-red-600 hover:bg-red-50 disabled:opacity-50 dark:border-red-900 dark:hover:bg-red-950/40"
				@click="onDelete">Delete</button>
		</div>

		<section v-if="schedule.history.length" aria-label="Recent runs" class="text-sm">
			<h3 class="mb-1 font-medium text-gray-700 dark:text-gray-200">Recent runs</h3>
			<ul data-testid="schedule-runs" class="divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-gray-800 dark:border-gray-800">
				<li v-for="run in schedule.history" :key="run.fireTime" class="flex items-center gap-2 px-3 py-1.5 text-xs">
					<span class="h-2 w-2 shrink-0 rounded-full" :class="scheduleRunDot(run.status)" />
					<span class="w-16 shrink-0 text-gray-500">{{ run.status }}</span>
					<span class="min-w-0 flex-1 truncate text-gray-700 dark:text-gray-200" :title="run.summary">{{ run.summary || run.reason }}</span>
					<span class="shrink-0 tabular-nums text-gray-400">{{ runDuration(run.durationMS) }}</span>
					<span class="shrink-0 text-gray-400">{{ formatFire(run.fireTime) }}</span>
				</li>
			</ul>
		</section>
	</form>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useAvailableModels } from "../../composables/useAvailableModels";
import {
	formatFire,
	type ProjectSchedule,
	runDuration,
	type ScheduleTrigger,
	scheduleRunDot,
} from "../../lib/projects";
import ModelSelector from "../ModelSelector.vue";

const props = defineProps<{
	schedule: ProjectSchedule;
	agents: string[];
	save: (s: ProjectSchedule) => Promise<void>;
	remove: (s: ProjectSchedule) => Promise<void>;
	runNow: (s: ProjectSchedule) => Promise<void>;
	preview: (s: ProjectSchedule) => Promise<string[]>;
}>();
const emit = defineEmits<{ saved: [id: string]; deleted: [] }>();
const { availableModelOptions, refreshCredentials } = useAvailableModels();
onMounted(() => void refreshCredentials());

const INPUT =
	"mt-1 block w-full rounded-lg border border-gray-200 bg-white px-2.5 py-1.5 text-sm text-gray-900 disabled:bg-gray-50 disabled:text-gray-500 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100 dark:disabled:bg-gray-800";
const TRIGGERS: { key: ScheduleTrigger; label: string }[] = [
	{ key: "once", label: "Once" },
	{ key: "interval", label: "Interval" },
	{ key: "daily", label: "Daily" },
	{ key: "weekly", label: "Weekly" },
	{ key: "cron", label: "Cron" },
];
const DAYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];
const localZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
const zones: string[] =
	(Intl as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.(
		"timeZone",
	) ?? [];

const draft = ref<ProjectSchedule>({ ...props.schedule });
watch(
	() => props.schedule,
	(next, prev) => {
		// Polls replace the schedule; only a different one resets the edits.
		if (next.id !== prev?.id || !next.id) draft.value = { ...next };
		else draft.value = { ...draft.value, history: next.history };
	},
);
const agentOptions = computed(() =>
	draft.value.agent && !props.agents.includes(draft.value.agent)
		? [draft.value.agent, ...props.agents]
		: props.agents,
);
watch(
	agentOptions,
	(list) => {
		if (!draft.value.agent && list.length) draft.value.agent = list[0];
	},
	{ immediate: true },
);

const days = computed(() =>
	draft.value.days
		.split(",")
		.map((d) => d.trim().toLowerCase())
		.filter(Boolean),
);
function toggleDay(d: string) {
	const next = days.value.includes(d)
		? days.value.filter((x) => x !== d)
		: [...days.value, d];
	draft.value.days = DAYS.filter((x) => next.includes(x)).join(",");
}

// datetime-local edits local wall time; the scheduler takes RFC 3339.
const startAtLocal = computed({
	get() {
		const t = Date.parse(draft.value.startAt);
		if (Number.isNaN(t)) return "";
		const d = new Date(t - new Date(t).getTimezoneOffset() * 60_000);
		return d.toISOString().slice(0, 16);
	},
	set(v: string) {
		const t = Date.parse(v);
		draft.value.startAt = Number.isNaN(t) ? "" : new Date(t).toISOString();
	},
});

const nextRuns = ref<string[]>([]);
const previewError = ref("");
let previewTimer: ReturnType<typeof setTimeout> | undefined;
watch(
	() =>
		[
			draft.value.trigger,
			draft.value.schedule,
			draft.value.every,
			draft.value.at,
			draft.value.days,
			draft.value.startAt,
			draft.value.timezone,
		].join("|"),
	() => {
		clearTimeout(previewTimer);
		previewTimer = setTimeout(async () => {
			try {
				nextRuns.value = await props.preview(draft.value);
				previewError.value = "";
			} catch (e) {
				nextRuns.value = [];
				previewError.value = e instanceof Error ? e.message : String(e);
			}
		}, 250);
	},
	{ immediate: true },
);

const busy = ref(false);
const error = ref("");
async function act(fn: () => Promise<void>) {
	busy.value = true;
	try {
		await fn();
		error.value = "";
		return true;
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
		return false;
	} finally {
		busy.value = false;
	}
}

async function onSave() {
	const s = draft.value;
	if (await act(() => props.save(s))) {
		emit("saved", s.id || `${s.agent}/${s.name.trim()}`);
	}
}

async function onDelete() {
	if (!window.confirm(`Delete the schedule "${draft.value.name}"?`)) return;
	if (await act(() => props.remove(props.schedule))) emit("deleted");
}
</script>
