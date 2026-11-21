<template>
	<AlertDialogRoot :open="!!leavePrompt" @update:open="(v) => { if (!v) answerLeavePrompt(false) }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent data-testid="unsaved-changes-dialog"
				class="fixed left-1/2 top-1/2 z-50 w-[calc(100%-1.5rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">
					Unsaved changes
				</AlertDialogTitle>
				<AlertDialogDescription class="mt-2 text-sm text-gray-600 dark:text-gray-400">
					You have unsaved changes. Leave this page?
				</AlertDialogDescription>
				<div class="mt-6 flex justify-end gap-3">
					<AlertDialogCancel
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						Stay
					</AlertDialogCancel>
					<button type="button"
						class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500"
						@click="answerLeavePrompt(true)">
						Leave
					</button>
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
import {
	answerLeavePrompt,
	leavePrompt,
} from "../composables/useUnsavedChanges";
</script>
