<template>
	<ComboboxRoot v-model:open="open" v-model:search-term="searchTerm" :model-value="selectedKeys" :multiple="multiple"
		:disabled="disabled" :filter-function="filter" :display-value="displayValue" class="relative w-full"
		@update:model-value="onChange">
		<ComboboxAnchor :data-testid="dataTestid" :data-value="dataValue" :data-state="open ? 'open' : 'closed'"
			:data-disabled="disabled ? '' : undefined"
			:class="[FIELD_BASE, FIELD[size], invalid ? FIELD_INVALID : FIELD_BORDER, 'cursor-text focus-within:border-blue-500 focus-within:ring-1 focus-within:ring-blue-500', multiple ? 'flex-wrap' : '', props.class]"
			@mousedown="onAnchorMouseDown">
			<template v-if="multiple">
				<span v-for="key in selectedKeys" :key="key" data-testid="combobox-chip"
					class="inline-flex max-w-[12rem] shrink-0 items-center gap-1 rounded-md border border-blue-100 bg-blue-50 px-1.5 py-0.5 text-xs font-medium text-blue-700 dark:border-blue-800 dark:bg-blue-900/30 dark:text-blue-300">
					<span class="truncate">{{ labelFor(key) }}</span>
					<button type="button" :aria-label="`Remove ${labelFor(key)}`" :disabled="disabled"
						class="rounded hover:text-blue-900 dark:hover:text-blue-100" @mousedown.stop.prevent
						@click.stop="removeKey(key)">
						<XMarkIcon class="h-3 w-3" aria-hidden="true" />
					</button>
				</span>
			</template>
			<ComboboxInput ref="inputRef" :id="id" :aria-label="ariaLabel" :aria-invalid="invalid ? 'true' : undefined"
				:placeholder="multiple ? (selectedKeys.length ? '' : placeholder) : placeholder"
				class="min-w-[4rem] flex-1 bg-transparent p-0 text-inherit outline-none placeholder:text-gray-400 dark:placeholder:text-gray-500"
				@focus="open = true" @click="open = true" @keydown.backspace="onBackspace" />
			<button v-if="clearable && !multiple && selectedKeys" type="button" aria-label="Clear selection"
				class="shrink-0 rounded text-gray-400 hover:text-gray-700 dark:text-gray-500 dark:hover:text-gray-200"
				@mousedown.stop.prevent @click.stop="clear">
				<XMarkIcon class="h-4 w-4" aria-hidden="true" />
			</button>
			<ComboboxTrigger aria-label="Show options" tabindex="-1" class="shrink-0" @mousedown.stop>
				<ChevronDownIcon :class="CHEVRON" aria-hidden="true" />
			</ComboboxTrigger>
		</ComboboxAnchor>
		<input v-if="name && !multiple" type="hidden" :name="name" :value="modelValue ?? ''" />
		<ComboboxPortal>
			<ComboboxContent position="popper" :side-offset="4" :collision-padding="8" data-testid="select-content"
				:data-state="open ? 'open' : 'closed'"
				:class="[CONTENT_BASE, CONTENT[size], 'w-[var(--radix-combobox-trigger-width)] max-w-[calc(100vw-1rem)] max-h-[min(20rem,var(--radix-combobox-content-available-height))]']">
				<ComboboxViewport class="max-h-[inherit] overflow-y-auto p-1">
					<ComboboxEmpty :class="EMPTY">{{ emptyText ?? "No matching options" }}</ComboboxEmpty>
					<ComboboxGroup v-for="(group, gi) in groups" :key="`${gi}-${group.label}`">
						<ComboboxLabel v-if="group.label" :class="GROUP_LABEL">{{ group.label }}</ComboboxLabel>
						<ComboboxItem v-for="(option, oi) in group.options" :key="optionKey(option.value)"
							:value="optionKey(option.value)" :disabled="option.disabled"
							:aria-labelledby="`${uid}-${gi}-${oi}-l`"
							:aria-describedby="option.description ? `${uid}-${gi}-${oi}-d` : undefined"
							:class="[ITEM_BASE, ITEM[size], 'group/item']">
							<span class="min-w-0 flex-1">
								<span :id="`${uid}-${gi}-${oi}-l`" class="block truncate" data-option-label>
									<slot name="option" :option="option">{{ option.label }}</slot>
								</span>
								<span v-if="option.description" :id="`${uid}-${gi}-${oi}-d`" :class="DESCRIPTION">{{ option.description }}</span>
							</span>
							<span class="flex h-4 w-4 shrink-0 items-center">
								<ComboboxItemIndicator data-testid="select-check">
									<CheckIcon :class="CHECK" aria-hidden="true" />
								</ComboboxItemIndicator>
							</span>
						</ComboboxItem>
					</ComboboxGroup>
				</ComboboxViewport>
			</ComboboxContent>
		</ComboboxPortal>
	</ComboboxRoot>
</template>

<script setup lang="ts">
// The app's searchable dropdown: a styled radix-vue Combobox for long lists
// (timezones, models, sessions, channels). With multiple, the chosen options
// show as removable chips in the field.
import CheckIcon from "@heroicons/vue/20/solid/CheckIcon";
import ChevronDownIcon from "@heroicons/vue/20/solid/ChevronDownIcon";
import XMarkIcon from "@heroicons/vue/20/solid/XMarkIcon";
import {
	ComboboxAnchor,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxGroup,
	ComboboxInput,
	ComboboxItem,
	ComboboxItemIndicator,
	ComboboxLabel,
	ComboboxPortal,
	ComboboxRoot,
	ComboboxTrigger,
	ComboboxViewport,
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
import {
	groupOptions,
	matchesSearch,
	optionKey,
	type SelectOption,
} from "./types";

const props = withDefaults(
	defineProps<{
		modelValue: string | string[] | null | undefined;
		options: SelectOption[];
		multiple?: boolean;
		placeholder?: string;
		emptyText?: string;
		/** Shows a button that clears a single selection. */
		clearable?: boolean;
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

const emit = defineEmits<{ "update:modelValue": [value: string | string[]] }>();

const open = ref(false);
const uid = useId();
const searchTerm = ref("");
const inputRef = ref<{ $el: HTMLInputElement } | null>(null);

const groups = computed(() => groupOptions(props.options));
const byKey = computed(
	() => new Map(props.options.map((o) => [optionKey(o.value), o])),
);

const selectedKeys = computed<string | string[]>(() => {
	if (props.multiple)
		return Array.isArray(props.modelValue)
			? props.modelValue.map(optionKey)
			: [];
	const v = typeof props.modelValue === "string" ? props.modelValue : "";
	return v ? optionKey(v) : "";
});

const dataValue = computed(() =>
	Array.isArray(props.modelValue)
		? props.modelValue.join(",")
		: (props.modelValue ?? ""),
);

function labelFor(key: string): string {
	return byKey.value.get(key)?.label ?? (key === "\u0000empty" ? "" : key);
}

function displayValue(key: unknown): string {
	return typeof key === "string" && key ? labelFor(key) : "";
}

function filter(keys: unknown[], term: string): unknown[] {
	return keys.filter((k) => {
		const option = byKey.value.get(String(k));
		return option ? matchesSearch(option, term) : false;
	});
}

function valueFor(key: string): string {
	return String(byKey.value.get(key)?.value ?? key);
}

function onChange(next: unknown) {
	if (props.multiple) {
		const keys = Array.isArray(next) ? (next as string[]) : [];
		emit("update:modelValue", keys.map(valueFor));
		searchTerm.value = "";
		return;
	}
	if (typeof next === "string") emit("update:modelValue", valueFor(next));
}

function removeKey(key: string) {
	if (!Array.isArray(props.modelValue)) return;
	emit(
		"update:modelValue",
		props.modelValue.filter((v) => optionKey(v) !== key),
	);
}

function clear() {
	emit("update:modelValue", "");
	searchTerm.value = "";
}

function onBackspace() {
	if (searchTerm.value || !props.multiple) return;
	const keys = selectedKeys.value as string[];
	const last = keys[keys.length - 1];
	if (last) removeKey(last);
}

/** Clicking the field's padding or chips focuses the input, like a text field. */
function onAnchorMouseDown(event: MouseEvent) {
	const input = inputRef.value?.$el;
	if (!input || props.disabled || event.target === input) return;
	event.preventDefault();
	input.focus();
	open.value = true;
}
</script>
