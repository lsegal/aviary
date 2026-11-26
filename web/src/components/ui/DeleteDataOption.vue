<template>
	<div class="mt-3 rounded-lg border border-gray-200 px-3 py-2.5 dark:border-gray-700" :data-testid="testId">
		<label class="flex items-start gap-3 text-sm text-gray-700 dark:text-gray-300">
			<input v-model="checked" type="checkbox" :data-testid="`${testId}-checkbox`"
				class="mt-0.5 h-4 w-4 rounded border-gray-300 text-red-600 focus:ring-red-500 dark:border-gray-600 dark:bg-gray-800" />
			<span>
				<span class="font-medium text-gray-900 dark:text-white">Delete session data and files</span>
				<span class="block text-xs text-gray-500 dark:text-gray-400">{{ covers }}</span>
			</span>
		</label>
		<p class="mt-2 text-xs" :data-testid="`${testId}-summary`"
			:class="checked ? 'text-red-700 dark:text-red-400' : 'text-gray-600 dark:text-gray-400'">
			{{ summary }}
		</p>
	</div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useMCP } from "../../composables/useMCP";
import {
	type DataCounts,
	type DataOwnerKind,
	formatDataCounts,
} from "../../lib/deleteData";

// The "Delete session data and files" choice in a delete confirmation, for an
// object that owns sessions or files. It is checked by default and states
// what checking it deletes, counted by delete_preview, and what unchecking it
// keeps.

const props = withDefaults(
	defineProps<{
		kind: DataOwnerKind;
		/** The owning agent of a task or channel. */
		agent?: string;
		/** The object's name; counts load while it is set. */
		name: string;
		testId?: string;
	}>(),
	{ agent: "", testId: "delete-data" },
);
const checked = defineModel<boolean>({ default: true });

const { callTool } = useMCP();
const counts = ref<DataCounts | null>(null);

watch(
	() => [props.kind, props.agent, props.name] as const,
	async ([kind, agent, name]) => {
		counts.value = null;
		if (!name) return;
		try {
			const next: DataCounts = JSON.parse(
				await callTool("delete_preview", { kind, agent, name }),
			);
			if (props.name === name && typeof next?.sessions === "number")
				counts.value = next;
		} catch {
			// The counts only inform the choice; deleting still works.
		}
	},
	{ immediate: true },
);

const covers = computed(() => {
	if (props.kind === "agent")
		return "Its AGENTS.md, MEMORY.md, memory, notes, chat sessions, run logs, media and other files.";
	if (props.kind === "project")
		return "Its job records, job transcripts, and the clones no job still uses.";
	return "Its chat history and sessions, run logs, and media.";
});

const summary = computed(() => {
	if (!checked.value) {
		if (props.kind === "agent")
			return "Only removes the agent from the configuration. Its data stays on disk, so adding an agent with the same name brings it back.";
		if (props.kind === "project")
			return "Keeps its job records, transcripts and clones on disk.";
		return "Keeps its data on disk. Its sessions become regular chats, marked archived.";
	}
	if (!counts.value) return "Deletes its data too. This can't be undone.";
	const what = formatDataCounts(counts.value);
	return what
		? `Deletes ${what}. This can't be undone.`
		: "It has no saved sessions, runs or files yet.";
});
</script>
