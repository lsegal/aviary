<template>
	<div class="flex flex-col items-center gap-2 rounded-xl border border-dashed border-gray-300 bg-white px-4 py-8 text-center dark:border-gray-700 dark:bg-gray-900">
		<span v-if="$slots.icon" class="flex h-10 w-10 items-center justify-center rounded-full bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400" aria-hidden="true">
			<slot name="icon" />
		</span>
		<p v-if="title" class="text-sm font-semibold text-gray-900 dark:text-white">{{ title }}</p>
		<p class="max-w-md text-sm text-gray-500 dark:text-gray-400"><slot /></p>
		<div v-if="$slots.actions" class="mt-2 flex flex-wrap items-center justify-center gap-2">
			<slot name="actions" />
		</div>
	</div>
</template>

<script setup lang="ts">
// What an overview section shows when it has nothing to list: what would go
// there, and the action that adds the first one.

defineProps<{ title?: string }>();
</script>
