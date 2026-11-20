<template>
	<section id="delete" class="scroll-mt-24 rounded-xl border border-red-200 bg-white dark:border-red-900/60 dark:bg-gray-950">
		<h2
			class="flex flex-wrap items-baseline gap-x-2 border-b border-red-200 px-5 py-3 text-base font-semibold text-gray-900 dark:border-red-900/60 dark:text-white">
			Delete <span class="text-sm font-normal text-gray-500 dark:text-gray-400">Stop watching {{ target }} and remove it from aviary.yaml</span>
		</h2>
		<div class="flex flex-wrap items-center gap-3 p-5">
			<p class="min-w-0 flex-1 text-sm text-gray-600 dark:text-gray-400">
				The GitHub repository, its issues and pull requests, and the clones on disk are left alone.
			</p>
			<button type="button"
				class="rounded-lg border border-red-300 px-3 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-950/40"
				@click="openDialog">Delete project</button>
		</div>
	</section>

	<AlertDialogRoot :open="open" @update:open="(v) => { if (!v && !deleting) open = false }">
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
import { ref, toRef } from "vue";
import { useRemoveProject } from "../../composables/useProjectSettings";

const props = defineProps<{ target: string }>();
const emit = defineEmits<{ deleted: [stopped: number[]] }>();

const { inFlight, loadInFlight, remove } = useRemoveProject(
	toRef(props, "target"),
);
const open = ref(false);
const deleting = ref(false);
const error = ref("");

async function openDialog() {
	error.value = "";
	open.value = true;
	try {
		await loadInFlight();
	} catch {
		// The list only informs the confirmation; deleting still stops the jobs.
	}
}

async function confirm() {
	deleting.value = true;
	error.value = "";
	try {
		const stopped = await remove();
		open.value = false;
		emit("deleted", stopped);
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		deleting.value = false;
	}
}
</script>
