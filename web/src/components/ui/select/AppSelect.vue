<template>
	<SelectRoot v-model:open="open" :model-value="selectedKey" :disabled="disabled" @update:model-value="onChange">
		<SelectTrigger :id="id" :aria-label="ariaLabel" :aria-invalid="invalid ? 'true' : undefined"
			:data-testid="dataTestid" :data-value="modelValue === undefined || modelValue === null ? '' : String(modelValue)"
			:class="[FIELD_BASE, FIELD[size], invalid ? FIELD_INVALID : FIELD_BORDER, props.class]">
			<span class="min-w-0 flex-1 truncate" :class="selected ? '' : 'text-gray-400 dark:text-gray-500'">
				<slot name="value" :option="selected">{{ selected?.label ?? placeholder ?? "Select…" }}</slot>
			</span>
			<SelectIcon as-child>
				<ChevronDownIcon :class="CHEVRON" aria-hidden="true" />
			</SelectIcon>
		</SelectTrigger>
		<input v-if="name" type="hidden" :name="name" :value="modelValue ?? ''" />
		<SelectPortal>
			<SelectContent position="popper" :side-offset="4" :collision-padding="8" data-testid="select-content"
				:class="[CONTENT_BASE, CONTENT[size], 'min-w-[var(--radix-select-trigger-width)] max-w-[calc(100vw-1rem)] max-h-[min(20rem,var(--radix-select-content-available-height))]']">
				<SelectViewport class="p-1">
					<div v-if="!options.length" :class="EMPTY">{{ emptyText ?? "No options" }}</div>
					<SelectGroup v-for="(group, gi) in groups" :key="`${gi}-${group.label}`">
						<SelectLabel v-if="group.label" :class="GROUP_LABEL">{{ group.label }}</SelectLabel>
						<SelectItem v-for="(option, oi) in group.options" :key="optionKey(option.value)"
							:value="optionKey(option.value)" :text-value="option.label" :disabled="option.disabled"
							:aria-labelledby="`${uid}-${gi}-${oi}-l`"
							:aria-describedby="option.description ? `${uid}-${gi}-${oi}-d` : undefined"
							:class="[ITEM_BASE, ITEM[size], 'group/item']">
							<span class="min-w-0 flex-1">
								<SelectItemText :id="`${uid}-${gi}-${oi}-l`" data-option-label>
									<slot name="option" :option="option">{{ option.label }}</slot>
								</SelectItemText>
								<span v-if="option.description" :id="`${uid}-${gi}-${oi}-d`" :class="DESCRIPTION">{{ option.description }}</span>
							</span>
							<span class="flex h-4 w-4 shrink-0 items-center">
								<SelectItemIndicator data-testid="select-check">
									<CheckIcon :class="CHECK" aria-hidden="true" />
								</SelectItemIndicator>
							</span>
						</SelectItem>
					</SelectGroup>
				</SelectViewport>
			</SelectContent>
		</SelectPortal>
	</SelectRoot>
</template>

<script setup lang="ts" generic="V extends string | number">
// The app's dropdown: a styled radix-vue Select whose trigger matches the
// app's text inputs. Use AppCombobox instead for long, searchable lists.
import CheckIcon from "@heroicons/vue/20/solid/CheckIcon";
import ChevronDownIcon from "@heroicons/vue/20/solid/ChevronDownIcon";
import {
	SelectContent,
	SelectGroup,
	SelectIcon,
	SelectItem,
	SelectItemIndicator,
	SelectItemText,
	SelectLabel,
	SelectPortal,
	SelectRoot,
	SelectTrigger,
	SelectViewport,
} from "radix-vue";
import { computed, type HTMLAttributes, ref, useId } from "vue";
import {
	CHECK,
	CHEVRON,
	CONTENT,
	CONTENT_BASE,
	DESCRIPTION,
	EMPTY,
	FIELD,
	FIELD_BASE,
	FIELD_BORDER,
	FIELD_INVALID,
	GROUP_LABEL,
	ITEM,
	ITEM_BASE,
	type SelectSize,
} from "./styles";
import { groupOptions, optionKey, type SelectOption } from "./types";

const props = withDefaults(
	defineProps<{
		modelValue: V | null | undefined;
		options: SelectOption<V>[];
		placeholder?: string;
		/** Shown in the open popover when there are no options. */
		emptyText?: string;
		/** Submitted with an enclosing form under this name. */
		name?: string;
		id?: string;
		ariaLabel?: string;
		dataTestid?: string;
		disabled?: boolean;
		invalid?: boolean;
		size?: SelectSize;
		class?: HTMLAttributes["class"];
	}>(),
	{ size: "md" },
);

const emit = defineEmits<{ "update:modelValue": [value: V] }>();

const open = ref(false);
const uid = useId();
const groups = computed(() => groupOptions(props.options));
const selected = computed(() =>
	props.modelValue === undefined || props.modelValue === null
		? undefined
		: props.options.find(
				(o) => optionKey(o.value) === optionKey(props.modelValue as V),
			),
);
// An unmatched value shows the placeholder rather than a blank trigger.
const selectedKey = computed(() =>
	selected.value ? optionKey(selected.value.value) : "",
);

function onChange(key: string) {
	const option = props.options.find((o) => optionKey(o.value) === key);
	if (option) emit("update:modelValue", option.value);
}
</script>
