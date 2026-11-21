<template>
	<AppSelect :id="id" :data-testid="dataTestid" :model-value="selectedValue" :options="options"
		:disabled="disabled" :invalid="hasMissingSecret" @update:model-value="select">
		<template #value="{ option }">
			<span class="flex min-w-0 items-center gap-3">
				<span class="min-w-0 flex-1 truncate font-medium">{{ option?.label ?? emptyLabel }}</span>
				<span v-if="option && badgeFor(option.value)" :class="badgeClass(badgeFor(option.value)?.tone)"
					class="shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em]">
					{{ badgeFor(option.value)?.label }}
				</span>
			</span>
		</template>
		<template #option="{ option }">
			<span class="flex min-w-0 items-center gap-3">
				<span class="min-w-0 flex-1 truncate">{{ option.label }}</span>
				<span v-if="badgeFor(option.value)" :class="badgeClass(badgeFor(option.value)?.tone)"
					class="shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em]">
					{{ badgeFor(option.value)?.label }}
				</span>
			</span>
		</template>
	</AppSelect>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { AppSelect, type SelectOption } from "./ui/select";

type BadgeTone = "secret" | "inline" | "missing";

interface SecretOption extends SelectOption {
	badge?: string;
	badgeTone?: BadgeTone;
}

const ADD_VALUE = "__add_secret__";
const RAW_VALUE = "__raw_secret__";
const MISSING_VALUE = "__missing_secret__";

const props = defineProps<{
	modelValue: string;
	secrets: string[];
	emptyLabel?: string;
	addLabel?: string;
	id?: string;
	dataTestid?: string;
	disabled?: boolean;
}>();

const emit = defineEmits<{
	(event: "update:modelValue", value: string): void;
	(event: "add-secret"): void;
}>();

function maskInlineSecret(value: string): string {
	const trimmed = value.trim();
	if (!trimmed) return "";
	const prefix = trimmed.slice(0, Math.min(7, trimmed.length));
	return `${prefix}****`;
}

const selectedValue = computed(() => {
	const trimmed = props.modelValue?.trim() ?? "";
	if (!trimmed) return "";
	if (trimmed.startsWith("auth:")) {
		return props.secrets.includes(trimmed.slice(5)) ? trimmed : MISSING_VALUE;
	}
	return RAW_VALUE;
});

const options = computed<SecretOption[]>(() => {
	const result: SecretOption[] = [
		{ value: "", label: props.emptyLabel ?? "No secret selected" },
	];
	const trimmed = props.modelValue?.trim() ?? "";
	if (
		trimmed.startsWith("auth:") &&
		!props.secrets.includes(trimmed.slice(5))
	) {
		result.push({
			value: MISSING_VALUE,
			label: trimmed.slice(5),
			badge: "Missing Secret",
			badgeTone: "missing",
		});
	}
	if (trimmed && !trimmed.startsWith("auth:")) {
		result.push({
			value: RAW_VALUE,
			label: maskInlineSecret(trimmed),
			badge: "Inline",
			badgeTone: "inline",
		});
	}
	for (const name of props.secrets) {
		result.push({
			value: `auth:${name}`,
			label: name,
			badge: "Secret",
			badgeTone: "secret",
		});
	}
	result.push({
		value: ADD_VALUE,
		label: props.addLabel ?? "Add new secret",
	});
	return result;
});

const hasMissingSecret = computed(() => selectedValue.value === MISSING_VALUE);

function select(value: string) {
	if (value === ADD_VALUE) {
		emit("add-secret");
		return;
	}
	if (value === RAW_VALUE || value === MISSING_VALUE) {
		return;
	}
	emit("update:modelValue", value);
}

function badgeFor(value: string) {
	const option = options.value.find((o) => o.value === value);
	return option?.badge ? { label: option.badge, tone: option.badgeTone } : null;
}

function badgeClass(tone?: BadgeTone, selected = false): string {
	if (tone === "secret") {
		return selected
			? "bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-200"
			: "bg-amber-50 text-amber-700 dark:bg-amber-950 dark:text-amber-300";
	}
	if (tone === "inline") {
		return selected
			? "bg-gray-200 text-gray-800 dark:bg-gray-700 dark:text-gray-100"
			: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300";
	}
	if (tone === "missing") {
		return selected
			? "bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-200"
			: "bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-300";
	}
	return "";
}
</script>
