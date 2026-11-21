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

		<TriggerEditor :model-value="draft" :preview="preview" @update:model-value="Object.assign(draft, $event)" />

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
					<span class="h-2 w-2 shrink-0 rounded-full" :class="runDot(run.status)" />
					<span class="w-16 shrink-0 text-gray-500">{{ run.status }}</span>
					<span class="min-w-0 flex-1 truncate text-gray-700 dark:text-gray-200" :title="run.summary">{{ run.summary || run.reason }}</span>
					<span class="shrink-0 tabular-nums text-gray-400">{{ duration(run.durationMS) }}</span>
					<span class="shrink-0 text-gray-400">{{ formatFire(run.fireTime) }}</span>
				</li>
			</ul>
		</section>
	</form>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useAvailableModels } from "../../composables/useAvailableModels";
import type { ProjectSchedule } from "../../lib/projects";
import type { TriggerFields } from "../../lib/trigger";
import ModelSelector from "../ModelSelector.vue";
import TriggerEditor from "../TriggerEditor.vue";

const props = defineProps<{
	schedule: ProjectSchedule;
	agents: string[];
	save: (s: ProjectSchedule) => Promise<void>;
	remove: (s: ProjectSchedule) => Promise<void>;
	runNow: (s: ProjectSchedule) => Promise<void>;
	preview: (s: TriggerFields) => Promise<string[]>;
}>();
const emit = defineEmits<{ saved: [id: string]; deleted: [] }>();
const { availableModelOptions, refreshCredentials } = useAvailableModels();
onMounted(() => void refreshCredentials());

const INPUT =
	"mt-1 block w-full rounded-lg border border-gray-200 bg-white px-2.5 py-1.5 text-sm text-gray-900 disabled:bg-gray-50 disabled:text-gray-500 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100 dark:disabled:bg-gray-800";
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

function duration(ms: number) {
	if (!ms) return "";
	const s = Math.round(ms / 1000);
	return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

function runDot(status: string) {
	switch (status) {
		case "completed":
			return "bg-green-500";
		case "failed":
			return "bg-red-500";
		case "running":
		case "queued":
		case "retrying":
			return "bg-blue-500";
		default:
			return "bg-gray-400";
	}
}

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
