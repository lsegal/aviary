<template>
	<span v-if="!editing" class="inline">
		<template v-if="filter">Issues matching <code data-testid="issue-filter" class="font-mono"
				title="Double-click to edit this project's issue filter" @dblclick="open">{{ filter }}</code></template>
		<template v-else>All open issues</template>
		<span v-if="custom" data-testid="issue-filter-custom"
			class="ml-1 inline-flex items-center rounded-full bg-blue-50 px-1.5 text-xs font-medium text-blue-700 align-middle dark:bg-blue-950/50 dark:text-blue-300"
			title="This project has its own issue filter instead of the shared one">custom</span>
		<button type="button" data-testid="issue-filter-edit" :aria-label="EDIT_LABEL" :title="EDIT_LABEL"
			class="ml-0.5 inline-flex items-center justify-center rounded p-1 align-middle text-gray-400 hover:bg-gray-100 hover:text-gray-700 focus-visible:outline focus-visible:outline-2 focus-visible:outline-blue-500 dark:text-gray-500 dark:hover:bg-gray-800 dark:hover:text-gray-200"
			@click="open">
			<PencilIcon aria-hidden="true" class="h-3.5 w-3.5" />
		</button>
	</span>
	<form v-else data-testid="issue-filter-editor" class="flex w-full min-w-0 flex-wrap items-center gap-1 py-0.5 align-middle sm:inline-flex sm:w-auto sm:max-w-full"
		@submit.prevent="save">
		<label :for="inputId" class="sr-only">Issue filter for {{ target }}</label>
		<input :id="inputId" ref="input" v-model="draft" data-testid="issue-filter-input" type="text"
			spellcheck="false" autocomplete="off" autocapitalize="off" :aria-invalid="!!error"
			:aria-describedby="statusId"
			class="min-w-0 flex-1 basis-40 rounded border sm:w-72 sm:flex-none px-1.5 py-0.5 font-mono text-sm text-gray-900 focus:outline-none focus:ring-2 dark:bg-gray-900 dark:text-gray-100"
			:class="error ? 'border-red-400 focus:ring-red-400 dark:border-red-700' : 'border-gray-300 focus:ring-blue-500 dark:border-gray-700'"
			@keydown.esc.prevent="cancel" />
		<button type="submit" data-testid="issue-filter-save" aria-label="Save issue filter" title="Save (Enter)"
			:disabled="!canSave" :class="ICON_BUTTON_CLS">
			<CheckIcon aria-hidden="true" class="h-4 w-4" />
		</button>
		<button type="button" data-testid="issue-filter-cancel" aria-label="Cancel editing the issue filter"
			title="Cancel (Esc)" :disabled="saving" :class="ICON_BUTTON_CLS" @click="cancel">
			<XMarkIcon aria-hidden="true" class="h-4 w-4" />
		</button>
		<button v-if="custom" type="button" data-testid="issue-filter-reset" :disabled="saving"
			class="rounded px-1.5 py-0.5 text-xs text-gray-600 underline-offset-2 hover:underline disabled:opacity-50 dark:text-gray-300"
			@click="reset">Reset to default</button>
		<span :id="statusId" data-testid="issue-filter-status" role="status" aria-live="polite"
			class="basis-full text-xs sm:basis-auto"
			:class="error ? 'text-red-600 dark:text-red-400' : 'text-gray-500 dark:text-gray-400'">{{ statusText }}</span>
	</form>
</template>

<script setup lang="ts">
import CheckIcon from "@heroicons/vue/16/solid/CheckIcon";
import PencilIcon from "@heroicons/vue/16/solid/PencilIcon";
import XMarkIcon from "@heroicons/vue/16/solid/XMarkIcon";
import { computed, nextTick, ref, useId, watch } from "vue";
import { useMCP } from "../../composables/useMCP";
import { parseToolJSON } from "../../lib/projects";

// The Code Loop header's issue filter, editable inline for one project
// (#1213): saving sets the project's own filter, Reset to default removes it.
const props = defineProps<{
	target: string;
	/** The project's effective filter; empty picks up every open issue. */
	filter: string;
	/** Whether the project has its own filter instead of the shared one. */
	custom: boolean;
}>();
const emit = defineEmits<{
	/** The filter was saved or reset; the board should re-read status. */
	saved: [];
	"update:editing": [editing: boolean];
}>();

const EDIT_LABEL = "Edit issue filter for this project";
const ICON_BUTTON_CLS =
	"inline-flex shrink-0 items-center justify-center rounded border border-gray-200 p-1 text-gray-700 hover:bg-gray-50 disabled:opacity-40 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-800";
// Typing pauses this long before the dry-run search.
const CHECK_DELAY_MS = 400;

const { callTool } = useMCP();
const id = useId();
const inputId = `issue-filter-input-${id}`;
const statusId = `issue-filter-status-${id}`;
const editing = ref(false);
const draft = ref("");
const input = ref<HTMLInputElement | null>(null);
const checking = ref(false);
const saving = ref(false);
const matches = ref<number | null>(null);
const error = ref("");
let checkTimer: ReturnType<typeof setTimeout> | null = null;
// Only the latest check's answer is shown.
let checkSeq = 0;

const normalized = computed(() => draft.value.trim().replace(/\s+/g, " "));
const canSave = computed(
	() => !!normalized.value && !error.value && !saving.value,
);
const statusText = computed(() => {
	if (error.value) return error.value;
	if (saving.value) return "Saving…";
	if (!normalized.value) return "Enter a filter, or cancel";
	if (checking.value) return "Checking…";
	if (matches.value === null) return "";
	return `Matches ${matches.value} open ${matches.value === 1 ? "issue" : "issues"}`;
});

watch(editing, (value) => emit("update:editing", value));

async function open() {
	draft.value = props.filter;
	error.value = "";
	matches.value = null;
	editing.value = true;
	await nextTick();
	input.value?.focus();
	input.value?.select();
	scheduleCheck(0);
}

function close() {
	if (checkTimer) clearTimeout(checkTimer);
	checkSeq++;
	editing.value = false;
	checking.value = false;
}

function cancel() {
	if (saving.value) return;
	close();
}

function scheduleCheck(delay = CHECK_DELAY_MS) {
	if (checkTimer) clearTimeout(checkTimer);
	error.value = "";
	matches.value = null;
	checking.value = !!normalized.value;
	if (!normalized.value) return;
	checkTimer = setTimeout(() => void check(), delay);
}

async function check() {
	const seq = ++checkSeq;
	const filter = normalized.value;
	try {
		const res = parseToolJSON(
			await callTool("project_filter_check", { target: props.target, filter }),
		) as { matches?: number } | null;
		if (seq !== checkSeq) return;
		matches.value = Number(res?.matches ?? 0);
	} catch (e) {
		if (seq !== checkSeq) return;
		error.value = message(e);
	} finally {
		if (seq === checkSeq) checking.value = false;
	}
}

watch(draft, () => {
	if (editing.value) scheduleCheck();
});

async function update(args: Record<string, unknown>) {
	if (checkTimer) clearTimeout(checkTimer);
	checkSeq++;
	checking.value = false;
	saving.value = true;
	error.value = "";
	try {
		await callTool("project_update", { target: props.target, ...args });
		saving.value = false;
		close();
		emit("saved");
	} catch (e) {
		error.value = message(e);
		saving.value = false;
		await nextTick();
		input.value?.focus();
	}
}

async function save() {
	if (!canSave.value) return;
	// Saving the project's own filter unchanged changes nothing.
	if (props.custom && normalized.value === props.filter) {
		close();
		return;
	}
	await update({ filter: normalized.value });
}

async function reset() {
	await update({ reset_filter: true });
}

// The tool error reads "invalid filter: <reason>"; show the reason.
function message(e: unknown): string {
	const text = e instanceof Error ? e.message : String(e);
	return text.replace(/^invalid filter:\s*/i, "Invalid filter: ");
}

defineExpose({ open });
</script>
