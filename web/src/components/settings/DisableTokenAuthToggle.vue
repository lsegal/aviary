<template>
	<label class="flex cursor-pointer items-center gap-3">
		<input :checked="modelValue" type="checkbox" data-testid="disable-token-auth"
			class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800"
			@click="onClick" />
		<span class="text-sm text-gray-700 dark:text-gray-300">
			Disable token authentication
			<span class="ml-1 text-xs text-gray-400 dark:text-gray-500">(anyone who can reach the server can use it — not
				recommended)</span>
		</span>
	</label>

	<AlertDialogRoot :open="confirmOpen" @update:open="(v) => { if (!v) confirmOpen = false }">
		<AlertDialogPortal>
			<AlertDialogOverlay class="fixed inset-0 z-50 bg-black/50" />
			<AlertDialogContent data-testid="disable-token-auth-modal"
				class="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl bg-white p-6 shadow-2xl dark:bg-gray-900">
				<AlertDialogTitle class="text-base font-bold text-gray-900 dark:text-white">
					Disable token authentication?
				</AlertDialogTitle>
				<AlertDialogDescription class="mt-2 space-y-2 text-sm text-gray-600 dark:text-gray-400">
					<span class="block">
						With token authentication off, anyone who can reach the service can use it without a token,
						including running agents, reading sessions, and changing settings.
					</span>
					<span class="block">
						The risk is greater when "Expose service externally" is on or TLS is disabled.
					</span>
					<span v-if="externalAccess || noTls" data-testid="disable-token-auth-extra-risk"
						class="block rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-950 dark:text-red-300">
						<template v-if="externalAccess && noTls">This server is exposed externally and serves plain HTTP.</template>
						<template v-else-if="externalAccess">This server is exposed externally.</template>
						<template v-else>This server serves plain HTTP.</template>
					</span>
				</AlertDialogDescription>
				<div class="mt-6 flex justify-end gap-3">
					<AlertDialogCancel
						class="rounded-lg border border-gray-200 px-4 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-400 dark:hover:bg-gray-800">
						Cancel
					</AlertDialogCancel>
					<AlertDialogAction class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500"
						@click="confirm">
						Disable token authentication
					</AlertDialogAction>
				</div>
			</AlertDialogContent>
		</AlertDialogPortal>
	</AlertDialogRoot>
</template>

<script setup lang="ts">
import {
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogOverlay,
	AlertDialogPortal,
	AlertDialogRoot,
	AlertDialogTitle,
} from "radix-vue";
import { ref } from "vue";

const props = defineProps<{
	modelValue: boolean;
	externalAccess?: boolean;
	noTls?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [value: boolean] }>();

const confirmOpen = ref(false);

// Turning token authentication off waits for confirmation; turning it back on
// applies right away.
function onClick(event: MouseEvent) {
	if (props.modelValue) {
		emit("update:modelValue", false);
		return;
	}
	event.preventDefault();
	confirmOpen.value = true;
}

function confirm() {
	confirmOpen.value = false;
	emit("update:modelValue", true);
}
</script>
