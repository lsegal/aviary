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

	<ConfirmDialog v-model:open="confirmOpen" title="Disable token authentication?" :action="confirm"
		confirm-label="Disable token authentication" test-id="disable-token-auth-modal">
		<p>
			With token authentication off, anyone who can reach the service can use it without a token,
			including running agents, reading sessions, and changing settings.
		</p>
		<p>
			The risk is greater when "Expose service externally" is on or TLS is disabled.
		</p>
		<p v-if="externalAccess || noTls" data-testid="disable-token-auth-extra-risk"
			class="rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-950 dark:text-red-300">
			<template v-if="externalAccess && noTls">This server is exposed externally and serves plain HTTP.</template>
			<template v-else-if="externalAccess">This server is exposed externally.</template>
			<template v-else>This server serves plain HTTP.</template>
		</p>
	</ConfirmDialog>
</template>

<script setup lang="ts">
import { ref } from "vue";
import ConfirmDialog from "../ui/ConfirmDialog.vue";

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
	emit("update:modelValue", true);
}
</script>
