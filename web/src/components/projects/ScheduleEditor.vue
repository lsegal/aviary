<template>
	<form data-testid="schedule-editor" class="space-y-4" @submit.prevent="onSave">
		<div class="grid gap-3 sm:grid-cols-2">
			<label class="block text-sm">
				<span class="font-medium text-gray-700 dark:text-gray-200">Name</span>
				<input :value="draft.name" name="name" disabled :class="INPUT" />
			</label>
			<label class="block text-sm">
				<span class="font-medium text-gray-700 dark:text-gray-200">Owner agent</span>
				<input :value="draft.agent" name="agent" disabled :class="INPUT" />
			</label>
		</div>
		<p class="text-xs text-gray-500">
			A project scheduled task.
			<router-link data-testid="schedule-task-link" :to="agentTaskPath(draft.agent, draft.name)"
				class="text-blue-600 hover:underline dark:text-blue-400">Open in Scheduled Tasks</router-link>
		</p>
		<TaskPromptSection v-model:prompt="draft.prompt" v-model:precompile="draft.precompile" v-model:model="draft.model"
			id-prefix="schedule" :agent="draft.agent" type="project" :project="draft.project" :trigger="draft" :rows="5"
			:precompile-help="precompileHelp" show-model model-placeholder="Default project agent model"
			model-help="The project's agent runs this task with this model instead of its own. Leave empty to use the agent's model."
			prompt-placeholder="Triage issues opened since the last run and label them.">
			<template #hint>Runs in an isolated clone of {{ draft.project }} in place of /gh-fix.</template>
		</TaskPromptSection>

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
			<button type="button" :disabled="busy"
				class="rounded-lg border border-gray-200 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800"
				@click="act(() => props.runNow(schedule))">Run now</button>
			<button type="button" :disabled="busy"
				class="ml-auto rounded-lg border border-red-200 px-3 py-1.5 text-sm text-red-600 hover:bg-red-50 disabled:opacity-50 dark:border-red-900 dark:hover:bg-red-950/40"
				@click="onDelete">Delete</button>
		</div>

	</form>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useUnsavedChanges } from "../../composables/useUnsavedChanges";
import type { ProjectSchedule } from "../../lib/projects";
import type { TriggerFields } from "../../lib/trigger";
import { agentTaskPath } from "../../stores/settings";
import TaskPromptSection from "../TaskPromptSection.vue";
import TriggerEditor from "../TriggerEditor.vue";

const props = defineProps<{
	schedule: ProjectSchedule;
	save: (s: ProjectSchedule) => Promise<void>;
	remove: (s: ProjectSchedule) => Promise<void>;
	runNow: (s: ProjectSchedule) => Promise<void>;
	preview: (s: TriggerFields) => Promise<string[]>;
}>();
const emit = defineEmits<{ saved: [id: string]; deleted: [] }>();

const INPUT =
	"mt-1 block w-full rounded-lg border border-gray-200 bg-white px-2.5 py-1.5 text-sm text-gray-900 disabled:bg-gray-50 disabled:text-gray-500 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100 dark:disabled:bg-gray-800";
const draft = ref<ProjectSchedule>({ ...props.schedule });
/** The schedule's editable fields, without what polls keep refreshing. */
function editable(s: ProjectSchedule): string {
	return JSON.stringify({
		...s,
		history: undefined,
		nextRun: undefined,
		description: undefined,
	});
}
const saved = ref(editable(draft.value));
watch(
	() => props.schedule,
	(next, prev) => {
		// Polls replace the schedule; only a different one resets the edits.
		if (next.id !== prev?.id) {
			draft.value = { ...next };
			saved.value = editable(draft.value);
		} else draft.value = { ...draft.value, history: next.history };
	},
);
// The editor shows the schedule at ?scheduled=<id>, so picking another
// schedule or going back to the board leaves it as surely as another page does.
useUnsavedChanges(() => editable(draft.value) !== saved.value, {
	keeps: (to, from) =>
		to.path === from.path && to.query.scheduled === from.query.scheduled,
});
/** What precompiling does for this schedule, and whether its script exists yet. */
const precompileHelp = computed(() => {
	if (!draft.value.precompile)
		return "Compile a deterministic prompt into a script that runs in the clone without the model";
	if (props.schedule.precompile && props.schedule.compiled)
		return "Precompiled: runs execute its script in the clone with no model calls, and fall back to the model if it fails";
	return "Compiled on save when the prompt is deterministic; runs fall back to the model if its script fails";
});
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
		saved.value = editable(s);
		emit("saved", s.id);
	}
}

async function onDelete() {
	if (!window.confirm(`Delete the schedule "${draft.value.name}"?`)) return;
	if (await act(() => props.remove(props.schedule))) {
		saved.value = editable(draft.value);
		emit("deleted");
	}
}
</script>
