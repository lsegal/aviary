<template>
	<button type="button" :aria-label="label" :title="label" data-testid="sidebar-row-action"
		class="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-blue-600 text-white opacity-0 shadow-sm transition hover:bg-blue-500 focus:opacity-100 group-hover:opacity-100 group-focus-within:opacity-100 disabled:opacity-50 [@media(hover:none)]:opacity-100 dark:bg-blue-500 dark:hover:bg-blue-400"
		@click.stop.prevent="emit('click')">
		<PlusIcon aria-hidden="true" class="h-4 w-4" />
	</button>
</template>

<script setup lang="ts">
import PlusIcon from "@heroicons/vue/24/outline/PlusIcon";

// The sidebar's "row action +": a blue boxed plus at the right of a header
// row. Hover-capable pointers see it while the row (a "group") is hovered or
// focused; touch screens always do.

defineProps<{ label: string }>();
const emit = defineEmits<{ click: [] }>();
</script>
