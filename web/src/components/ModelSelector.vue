<template>
	<AppCombobox :model-value="modelValue" :options="comboOptions" :multiple="multiple" :placeholder="placeholder"
		:empty-text="emptyText ?? 'No matching options found'" :clearable="!multiple"
		@update:model-value="emit('update:modelValue', $event)">
		<template #option="{ option }">
			<span class="flex min-w-0 items-center gap-2">
				<span class="truncate">{{ option.label }}</span>
				<span v-if="modelSupportLabel(String(option.value))"
					class="shrink-0 rounded-full bg-gray-100 px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-[0.12em] text-gray-600 dark:bg-gray-800 dark:text-gray-300">
					{{ modelSupportLabel(String(option.value)) }}
				</span>
			</span>
		</template>
	</AppCombobox>
</template>

<script setup lang="ts">
// A searchable model (or other name) picker on the shared AppCombobox; with
// multiple, picks an ordered list such as fallbacks.
import { computed } from "vue";
import {
	lookupModel,
	modelDetailLabel,
	modelSupportLabel,
	SUPPORTED_MODELS,
} from "../constants/models";
import { AppCombobox, type SelectOption } from "./ui/select";

const props = defineProps<{
	modelValue: string | string[] | undefined;
	multiple?: boolean;
	placeholder?: string;
	options?: string[];
	emptyText?: string;
}>();

const emit = defineEmits<{ "update:modelValue": [value: string | string[]] }>();

const comboOptions = computed<SelectOption[]>(() => {
	const names = [...(props.options ?? SUPPORTED_MODELS)];
	// A saved value missing from the list still shows as chosen.
	const saved = Array.isArray(props.modelValue)
		? props.modelValue
		: props.modelValue
			? [props.modelValue]
			: [];
	for (const v of saved) if (!names.includes(v)) names.push(v);
	return names.map((name) => ({
		value: name,
		label: name,
		description: lookupModel(name) ? modelDetailLabel(name) : undefined,
		keywords: modelSupportLabel(name),
	}));
});
</script>
