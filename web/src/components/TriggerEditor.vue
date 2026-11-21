<template>
	<fieldset data-testid="trigger-editor" class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-gray-800">
		<legend class="px-1 text-sm font-medium text-gray-700 dark:text-gray-200">Trigger</legend>
		<div role="radiogroup" aria-label="Trigger" class="flex flex-wrap overflow-hidden rounded-lg border border-gray-200 text-sm dark:border-gray-700">
			<button v-for="t in triggers" :key="t.key" type="button" role="radio" :aria-checked="value.trigger === t.key"
				class="flex-1 px-3 py-1.5" :class="value.trigger === t.key ? 'bg-blue-600 text-white' : 'text-gray-600 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'"
				@click="set('trigger', t.key)">{{ t.label }}</button>
		</div>
		<p v-if="!value.trigger" class="text-xs text-gray-500">Runs only when triggered manually or by a watched file.</p>
		<template v-else>
			<div class="grid gap-3 sm:grid-cols-2">
				<label v-if="value.trigger === 'once'" class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">Start at</span>
					<input v-model="startAtLocal" name="start_at" type="datetime-local" required :class="INPUT" />
				</label>
				<label v-if="value.trigger === 'interval'" class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">Every</span>
					<input :value="value.every" name="every" required :class="INPUT" placeholder="30m, 2h, 1d" @input="setInput('every', $event)" />
				</label>
				<label v-if="value.trigger === 'daily' || value.trigger === 'weekly'" class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">At</span>
					<input :value="value.at" name="at" required :class="INPUT" placeholder="09:00 or 09:00,17:30" @input="setInput('at', $event)" />
				</label>
				<div v-if="value.trigger === 'weekly'" class="text-sm sm:col-span-2">
					<span class="text-gray-600 dark:text-gray-300">Days</span>
					<div class="mt-1 flex flex-wrap gap-1">
						<button v-for="d in DAYS" :key="d" type="button" :aria-pressed="days.includes(d)"
							class="rounded-md border px-2 py-1 text-xs capitalize"
							:class="days.includes(d) ? 'border-blue-600 bg-blue-600 text-white' : 'border-gray-200 text-gray-600 dark:border-gray-700 dark:text-gray-300'"
							@click="toggleDay(d)">{{ d }}</button>
					</div>
				</div>
				<label v-if="value.trigger === 'cron'" class="block text-sm sm:col-span-2">
					<span class="text-gray-600 dark:text-gray-300">Cron expression</span>
					<input :value="value.schedule" name="schedule" required :class="INPUT" class="font-mono" placeholder="0 9 * * 1-5" @input="setInput('schedule', $event)" />
				</label>
				<label class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">Timezone</span>
					<AppCombobox :model-value="value.timezone" name="timezone" :options="zoneOptions" class="mt-1" clearable
						:placeholder="`Local time (${localZone})`" empty-text="No matching timezones"
						@update:model-value="set('timezone', String($event))" />
				</label>
			</div>
			<div class="grid gap-3 sm:grid-cols-2">
				<label class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">If the last run is still going</span>
					<AppSelect :model-value="value.overlap" name="overlap" :options="OVERLAP_OPTIONS" class="mt-1"
						@update:model-value="set('overlap', $event)" />
				</label>
				<label class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">Runs missed while asleep or stopped</span>
					<AppSelect :model-value="value.missed" name="missed" :options="MISSED_OPTIONS" class="mt-1"
						@update:model-value="set('missed', $event)" />
				</label>
			</div>
			<div data-testid="trigger-preview" class="text-xs">
				<p class="font-medium text-gray-600 dark:text-gray-300">Next runs</p>
				<p v-if="previewError" class="text-red-600 dark:text-red-400">{{ previewError }}</p>
				<ol v-else-if="nextRuns.length" class="mt-1 space-y-0.5 font-mono text-gray-700 dark:text-gray-200">
					<li v-for="t in nextRuns" :key="t">{{ formatFire(t) }}</li>
				</ol>
				<p v-else class="text-gray-400">No upcoming runs.</p>
			</div>
		</template>
	</fieldset>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { formatFire } from "../lib/projects";
import type { TriggerFields } from "../lib/trigger";
import { AppCombobox, AppSelect, type SelectOption } from "./ui/select";

// One trigger editor for every surface that schedules a task: Projects
// schedules and agent tasks.
const props = defineProps<{
	modelValue: TriggerFields;
	preview: (s: TriggerFields) => Promise<string[]>;
	/** Offers a "None" tab for tasks that need no time trigger. */
	optional?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [value: TriggerFields] }>();

const INPUT =
	"mt-1 block w-full rounded-lg border border-gray-200 bg-white px-2.5 py-1.5 text-sm text-gray-900 disabled:bg-gray-50 disabled:text-gray-500 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100 dark:disabled:bg-gray-800";
const TRIGGERS: { key: TriggerFields["trigger"]; label: string }[] = [
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
const OVERLAP_OPTIONS: SelectOption<TriggerFields["overlap"]>[] = [
	{
		value: "queue",
		label: "Queue this run",
		description: "Starts it once the current run finishes",
	},
	{
		value: "skip",
		label: "Skip this run",
		description: "Drops it while the last run is still going",
	},
];
const MISSED_OPTIONS: SelectOption<TriggerFields["missed"]>[] = [
	{
		value: "skip",
		label: "Skip them",
		description: "Waits for the next scheduled time",
	},
	{
		value: "run_once",
		label: "Run once to catch up",
		description: "Runs a single missed occurrence after waking",
	},
];

const value = computed(() => props.modelValue);
// A saved zone the browser doesn't list still shows as chosen.
const zoneOptions = computed<SelectOption[]>(() => {
	const tz = value.value.timezone;
	const list = tz && !zones.includes(tz) ? [tz, ...zones] : zones;
	return list.map((z) => ({ value: z, label: z }));
});
const triggers = computed(() =>
	props.optional
		? [{ key: "" as const, label: "None" }, ...TRIGGERS]
		: TRIGGERS,
);

function set<K extends keyof TriggerFields>(key: K, v: TriggerFields[K]) {
	emit("update:modelValue", { ...props.modelValue, [key]: v });
}
function setInput(key: keyof TriggerFields, e: Event) {
	set(key, (e.target as HTMLInputElement).value as never);
}

const days = computed(() =>
	value.value.days
		.split(",")
		.map((d) => d.trim().toLowerCase())
		.filter(Boolean),
);
function toggleDay(d: string) {
	const next = days.value.includes(d)
		? days.value.filter((x) => x !== d)
		: [...days.value, d];
	set("days", DAYS.filter((x) => next.includes(x)).join(","));
}

// datetime-local edits local wall time; the scheduler takes RFC 3339.
const startAtLocal = computed({
	get() {
		const t = Date.parse(value.value.startAt);
		if (Number.isNaN(t)) return "";
		const d = new Date(t - new Date(t).getTimezoneOffset() * 60_000);
		return d.toISOString().slice(0, 16);
	},
	set(v: string) {
		const t = Date.parse(v);
		set("startAt", Number.isNaN(t) ? "" : new Date(t).toISOString());
	},
});

const nextRuns = ref<string[]>([]);
const previewError = ref("");
let previewTimer: ReturnType<typeof setTimeout> | undefined;
watch(
	() =>
		[
			value.value.trigger,
			value.value.schedule,
			value.value.every,
			value.value.at,
			value.value.days,
			value.value.startAt,
			value.value.timezone,
		].join("|"),
	() => {
		clearTimeout(previewTimer);
		previewTimer = setTimeout(async () => {
			try {
				nextRuns.value = await props.preview(value.value);
				previewError.value = "";
			} catch (e) {
				nextRuns.value = [];
				previewError.value = e instanceof Error ? e.message : String(e);
			}
		}, 250);
	},
	{ immediate: true },
);
onUnmounted(() => clearTimeout(previewTimer));
</script>
