<template>
	<!-- One-of-a-few choice shown as joined buttons, styled like the trigger editor's tabs. -->
	<div role="radiogroup" :aria-label="label"
		class="flex overflow-hidden rounded-lg border border-gray-200 text-sm dark:border-gray-700">
		<button v-for="o in options" :key="o.value" type="button" role="radio" :aria-checked="modelValue === o.value"
			:disabled="o.disabled" :title="o.disabled ? o.disabledReason : undefined"
			class="flex-1 px-3 py-1.5 disabled:cursor-not-allowed disabled:opacity-50"
			:class="modelValue === o.value ? 'bg-blue-600 text-white' : 'text-gray-600 enabled:hover:bg-gray-50 dark:text-gray-300 dark:enabled:hover:bg-gray-800'"
			@click="emit('update:modelValue', o.value)">{{ o.label }}</button>
	</div>
</template>

<script setup lang="ts" generic="T extends string">
export interface SegmentedOption<V extends string> {
	value: V;
	label: string;
	disabled?: boolean;
	/** Shown as the disabled option's tooltip. */
	disabledReason?: string;
}

defineProps<{
	modelValue: T;
	options: SegmentedOption<T>[];
	/** The group's accessible name. */
	label: string;
}>();
const emit = defineEmits<{ "update:modelValue": [value: T] }>();
</script>
