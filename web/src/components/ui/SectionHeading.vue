<template>
	<div class="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2">
		<h2 :id="id" class="flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
			{{ title }}
			<span v-if="count !== undefined"
				class="rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium tabular-nums text-gray-600 dark:bg-gray-800 dark:text-gray-300">{{ count }}</span>
		</h2>
		<div v-if="$slots.default" class="ml-auto flex min-w-0 flex-wrap items-center gap-2">
			<slot />
		</div>
	</div>
</template>

<script setup lang="ts">
// An overview section's heading: its title, how many items it holds, and its
// actions on the right.

defineProps<{ id?: string; title: string; count?: number }>();
</script>
