<template>
	<ConfirmDialog :open="!!workflow" :title="`Delete workflow ${workflow}?`" :action="remove"
		confirm-label="Delete" busy-label="Deleting…" test-id="workflow-delete-dialog"
		@update:open="(v) => { if (!v) emit('close') }">
		<p>
			This deletes the workflow's definition. It won't run again.
		</p>
		<p v-if="inFlight.length" data-testid="workflow-delete-runs"
			class="rounded-lg border border-dashed border-amber-300 px-3 py-2 text-amber-800 dark:border-amber-800 dark:text-amber-300">
			{{ inFlight.length === 1 ? "A run is" : `${inFlight.length} runs are` }} in progress or queued.
			Deleting stops {{ inFlight.length === 1 ? "it" : "them" }}, and the workflow won't run again.
		</p>
		<DeleteDataOption v-model="deleteData" kind="task" :agent="agent" :name="workflow ? key() : ''"
			test-id="workflow-delete-data" />
	</ConfirmDialog>
</template>

<script setup lang="ts">
import { ref, watch } from "vue";
import { useMCP } from "../../composables/useMCP";
import { useToasts } from "../../composables/useToasts";
import ConfirmDialog from "../ui/ConfirmDialog.vue";
import DeleteDataOption from "../ui/DeleteDataOption.vue";

// Confirms deleting a channel workflow, for both the workflows list and the
// edit form's Danger Zone. Deleting stops the workflow's pending and running
// jobs, which the dialog lists before asking.

/** The workflow to delete; the dialog is open while it is set. */
const props = defineProps<{ agent: string; workflow: string }>();
const emit = defineEmits<{ deleted: [workflow: string]; close: [] }>();

const { callTool } = useMCP();
const { show } = useToasts();
const inFlight = ref<string[]>([]);
const deleteData = ref(true);

const key = () => `${props.agent}/${props.workflow}`;

watch(
	() => props.workflow,
	async (workflow) => {
		inFlight.value = [];
		deleteData.value = true;
		if (!workflow) return;
		try {
			const jobs: { id: string; status: string }[] =
				JSON.parse(await callTool("job_list", { task: key() })) ?? [];
			if (props.workflow !== workflow) return;
			inFlight.value = jobs
				.filter((j) => j.status === "pending" || j.status === "in_progress")
				.map((j) => j.id);
		} catch {
			// The notice only informs the confirmation; deleting still stops runs.
		}
	},
	{ immediate: true },
);

async function remove() {
	const workflow = props.workflow;
	const name = key();
	// Deleting first keeps a new message from starting a run in between.
	await callTool("task_delete", { name, delete_data: deleteData.value });
	emit("deleted", workflow);
	try {
		await callTool("task_stop", { name });
	} catch (e) {
		show(
			`Deleted ${workflow}, but couldn't stop its runs: ${e instanceof Error ? e.message : String(e)}`,
			"error",
		);
	}
}
</script>
