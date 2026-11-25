<template>
	<AlertDialogRoot :open="open" @update:open="(v) => { if (!v) close() }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent :data-testid="testId"
				class="fixed left-1/2 top-1/2 z-50 max-h-[calc(100dvh-1.5rem)] w-[calc(100%-1.5rem)] max-w-md -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900"
				@escape-key-down="(e) => { if (busy) e.preventDefault() }">
				<AlertDialogTitle class="break-words text-base font-bold text-gray-900 dark:text-white">
					{{ title }}
				</AlertDialogTitle>
				<AlertDialogDescription as="div" class="mt-2 space-y-2 text-sm text-gray-600 dark:text-gray-400">
					<slot />
				</AlertDialogDescription>
				<p v-if="error" role="alert" :data-testid="testId && `${testId}-error`"
					class="mt-3 text-sm text-red-600 dark:text-red-400">{{ error }}</p>
				<div class="mt-6 flex flex-wrap justify-end gap-3">
					<AlertDialogCancel :disabled="busy" :data-testid="testId && `${testId}-cancel`"
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						{{ cancelLabel }}
					</AlertDialogCancel>
					<button type="button" :disabled="busy" :aria-busy="busy" :data-testid="testId && `${testId}-confirm`"
						class="rounded-lg px-4 py-2 text-sm font-semibold text-white disabled:opacity-50"
						:class="danger ? 'bg-red-600 hover:bg-red-500' : 'bg-blue-600 hover:bg-blue-500'"
						@click="confirm">{{ busy ? busyLabel : confirmLabel }}</button>
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
import { ref, watch } from "vue";

// The app's confirmation modal, in place of the browser's confirm(). It runs
// action itself, staying open and busy until it finishes, and shows its
// failure inside the modal instead of closing. Cancel and Esc close it
// without running anything. The slot is the dialog's description.

const props = withDefaults(
	defineProps<{
		open: boolean;
		title: string;
		/** Runs on confirm; the modal closes once it resolves. */
		action: () => unknown;
		confirmLabel?: string;
		busyLabel?: string;
		cancelLabel?: string;
		/** A red confirm button, for destructive actions. */
		danger?: boolean;
		testId?: string;
	}>(),
	{
		confirmLabel: "Confirm",
		busyLabel: "Working…",
		cancelLabel: "Cancel",
		danger: true,
		testId: undefined,
	},
);
const emit = defineEmits<{ "update:open": [open: boolean]; cancel: [] }>();

const busy = ref(false);
const error = ref("");

watch(
	() => props.open,
	(open) => {
		if (open) error.value = "";
	},
);

function close() {
	if (busy.value) return;
	emit("update:open", false);
	emit("cancel");
}

async function confirm() {
	busy.value = true;
	error.value = "";
	try {
		await props.action();
		emit("update:open", false);
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		busy.value = false;
	}
}
</script>
