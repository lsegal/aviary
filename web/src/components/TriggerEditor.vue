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
					<input :value="value.timezone" name="timezone" :list="zoneListId" :class="INPUT" :placeholder="localZone" @input="setInput('timezone', $event)" />
					<datalist :id="zoneListId">
						<option v-for="z in zones" :key="z" :value="z" />
					</datalist>
				</label>
			</div>
			<div class="grid gap-3 sm:grid-cols-2">
				<label class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">If the last run is still going</span>
					<select :value="value.overlap" name="overlap" :class="INPUT" @change="setInput('overlap', $event)">
						<option value="queue">Queue this run</option>
						<option value="skip">Skip this run</option>
					</select>
				</label>
				<label class="block text-sm">
					<span class="text-gray-600 dark:text-gray-300">Runs missed while asleep or stopped</span>
					<select :value="value.missed" name="missed" :class="INPUT" @change="setInput('missed', $event)">
						<option value="skip">Skip them</option>
						<option value="run_once">Run once to catch up</option>
					</select>
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
import { computed, onUnmounted, ref, useId, watch } from "vue";
import type { TriggerFields } from "../lib/trigger";

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
const zoneListId = `trigger-timezones-${useId()}`;

const value = computed(() => props.modelValue);
const triggers = computed(() =>
	props.optional ? [{ key: "" as const, label: "None" }, ...TRIGGERS] : TRIGGERS,
);

function set<K extends keyof TriggerFields>(key: K, v: TriggerFields[K]) {
	emit("update:modelValue", { ...props.modelValue, [key]: v });
}
function setInput(key: keyof TriggerFields, e: Event) {
	set(key, (e.target as HTMLInputElement | HTMLSelectElement).value as never);
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

function formatFire(iso: string) {
	const t = Date.parse(iso);
	return Number.isNaN(t)
		? iso
		: new Date(t).toLocaleString(undefined, {
				weekday: "short",
				month: "short",
				day: "numeric",
				hour: "2-digit",
				minute: "2-digit",
			});
}
</script>
