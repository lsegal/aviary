<template>
	<button type="button" role="switch" :aria-checked="modelValue" :aria-label="label"
		class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 disabled:cursor-default"
		:class="modelValue ? 'bg-blue-600 dark:bg-blue-500' : 'bg-gray-300 dark:bg-gray-700'"
		@click="emit('update:modelValue', !modelValue)">
		<span aria-hidden="true" class="inline-block h-5 w-5 rounded-full bg-white shadow transition-transform"
			:class="modelValue ? 'translate-x-[1.375rem]' : 'translate-x-0.5'" />
	</button>
</template>

<script setup lang="ts">
// An on/off switch, as a button with the switch role, for settings that
// apply at once.
defineProps<{ modelValue: boolean; label: string }>();
const emit = defineEmits<{ "update:modelValue": [value: boolean] }>();
</script>
