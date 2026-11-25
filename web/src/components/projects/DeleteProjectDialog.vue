<template>
	<ConfirmDialog :open="!!target" :title="`Delete ${target}?`" :action="confirm" confirm-label="Delete project"
		busy-label="Deleting…" test-id="delete-project-dialog" @update:open="(v) => { if (!v) emit('close') }">
		<p>
			Aviary stops watching <b class="font-mono font-medium text-gray-900 dark:text-white">{{ target }}</b>
			and removes it from aviary.yaml. The repository, its issues and pull requests, and the clones on disk are not touched.
		</p>
		<p v-if="inFlight.length" data-testid="delete-jobs" class="text-amber-700 dark:text-amber-400">
			{{ inFlight.length }} {{ inFlight.length === 1 ? "job is" : "jobs are" }} still in flight
			({{ inFlight.map((n) => `#${n}`).join(", ") }}). Deleting stops {{ inFlight.length === 1 ? "it" : "them" }}:
			running work is checkpointed and pushed first, and nothing resumes.
		</p>
	</ConfirmDialog>
</template>

<script setup lang="ts">
import { toRef, watch } from "vue";
import { useRemoveProject } from "../../composables/useProjectSettings";
import ConfirmDialog from "../ui/ConfirmDialog.vue";

/** The project to delete; the dialog is open while it is set. */
const props = defineProps<{ target: string }>();
const emit = defineEmits<{
	deleted: [target: string, stopped: number[]];
	close: [];
}>();

const { inFlight, loadInFlight, remove } = useRemoveProject(
	toRef(props, "target"),
);

watch(
	() => props.target,
	async (target) => {
		if (!target) return;
		try {
			await loadInFlight();
		} catch {
			// The list only informs the confirmation; deleting still stops the jobs.
		}
	},
	{ immediate: true },
);

async function confirm() {
	const target = props.target;
	const stopped = await remove();
	emit("deleted", target, stopped);
}
</script>
