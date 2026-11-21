<template>
	<AlertDialogRoot :open="!!target" @update:open="(v) => { if (!v && !deleting) emit('close') }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent
				class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">
					Delete {{ target }}?
				</AlertDialogTitle>
				<AlertDialogDescription class="mt-2 space-y-2 text-sm text-gray-600 dark:text-gray-400">
					<span class="block">
						Aviary stops watching <b class="font-mono font-medium text-gray-900 dark:text-white">{{ target }}</b>
						and removes it from aviary.yaml. The repository, its issues and pull requests, and the clones on disk are not touched.
					</span>
					<span v-if="inFlight.length" data-testid="delete-jobs" class="block text-amber-700 dark:text-amber-400">
						{{ inFlight.length }} {{ inFlight.length === 1 ? "job is" : "jobs are" }} still in flight
						({{ inFlight.map((n) => `#${n}`).join(", ") }}). Deleting stops {{ inFlight.length === 1 ? "it" : "them" }}:
						running work is checkpointed and pushed first, and nothing resumes.
					</span>
				</AlertDialogDescription>
				<p v-if="error" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
				<div class="mt-6 flex justify-end gap-3">
					<AlertDialogCancel :disabled="deleting"
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						Cancel
					</AlertDialogCancel>
					<button type="button" :disabled="deleting"
						class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500 disabled:opacity-50"
						@click="confirm">{{ deleting ? "Deleting…" : "Delete project" }}</button>
				</div>
			</AlertDialogContent>
		</AlertDialogPortal>
	</AlertDialogRoot>
</template>

<script setup lang="ts">
import {
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogOverlay,
	AlertDialogPortal,
	AlertDialogRoot,
	AlertDialogTitle,
} from "radix-vue";
import { ref, toRef, watch } from "vue";
import { useRemoveProject } from "../../composables/useProjectSettings";

/** The project to delete; the dialog is open while it is set. */
const props = defineProps<{ target: string }>();
const emit = defineEmits<{ deleted: [target: string, stopped: number[]]; close: [] }>();

const { inFlight, loadInFlight, remove } = useRemoveProject(
	toRef(props, "target"),
);
const deleting = ref(false);
const error = ref("");

watch(
	() => props.target,
	async (target) => {
		error.value = "";
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
	deleting.value = true;
	error.value = "";
	try {
		const target = props.target;
		const stopped = await remove();
		emit("deleted", target, stopped);
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		deleting.value = false;
	}
}
</script>
