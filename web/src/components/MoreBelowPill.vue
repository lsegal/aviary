<template>
	<div class="pointer-events-none absolute inset-x-0 bottom-3 z-10 flex justify-center">
		<button type="button" v-bind="$attrs"
			class="pointer-events-auto rounded-full border border-gray-300 bg-white px-3 py-1 text-xs font-medium text-gray-700 shadow-sm hover:bg-gray-100 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200 dark:hover:bg-gray-800"
			@click="emit('click')">
			<template v-if="count">{{ count }} new · </template>More below ↓
		</button>
	</div>
</template>

<script setup lang="ts">
// The floating "More below" button of a scrolled-up chat or log view
// (useAutoFollow). Its parent must be positioned, with the scroller filling
// it to the bottom.

defineOptions({ inheritAttrs: false });
defineProps<{ count?: number }>();
const emit = defineEmits<{ click: [] }>();
</script>
