<template>
  <div ref="root" class="relative w-full">
    <button
      :id="id"
      :data-testid="dataTestid"
      type="button"
      :disabled="disabled"
      :aria-expanded="open ? 'true' : 'false'"
      aria-haspopup="listbox"
      class="flex w-full items-center gap-3 rounded-lg border bg-white px-3 py-2 text-left transition focus:outline-none focus:ring-1 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-gray-800"
      :class="triggerClass"
      @click="toggle"
    >
      <span class="min-w-0 flex-1 truncate text-sm font-medium text-gray-900 dark:text-white">
        {{ selectedOption?.label ?? emptyLabel }}
      </span>
      <span
        v-if="selectedOption?.badge"
        :class="badgeClass(selectedOption.badgeTone)"
        class="shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em]"
      >
        {{ selectedOption.badge }}
      </span>
      <svg
        class="h-4 w-4 shrink-0 text-gray-400 transition-transform dark:text-gray-500"
        :class="open ? 'rotate-180' : ''"
        viewBox="0 0 20 20"
        fill="currentColor"
        aria-hidden="true"
      >
        <path
          fill-rule="evenodd"
          d="M5.23 7.21a.75.75 0 0 1 1.06.02L10 11.168l3.71-3.938a.75.75 0 1 1 1.08 1.04l-4.25 4.5a.75.75 0 0 1-1.08 0l-4.25-4.5a.75.75 0 0 1 .02-1.06Z"
          clip-rule="evenodd"
        />
      </svg>
    </button>

    <div
      v-if="open"
      class="absolute z-50 mt-1 w-full overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xl dark:border-gray-700 dark:bg-gray-900"
    >
      <div class="max-h-72 overflow-y-auto p-1">
        <button
          v-for="option in options"
          :key="option.value"
          type="button"
          role="option"
          :aria-selected="option.value === selectedValue ? 'true' : 'false'"
          class="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition"
          :class="
            option.value === selectedValue
              ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
              : 'text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'
          "
          @click="select(option.value)"
        >
          <span class="min-w-0 flex-1 truncate text-sm font-medium">{{ option.label }}</span>
          <span
            v-if="option.badge"
            :class="badgeClass(option.badgeTone, option.value === selectedValue)"
            class="shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em]"
          >
            {{ option.badge }}
          </span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";

type BadgeTone = "secret" | "inline" | "missing";

interface SecretOption {
	value: string;
	label: string;
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

const root = ref<HTMLElement | null>(null);
const open = ref(false);

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

const selectedOption = computed(
	() =>
		options.value.find((option) => option.value === selectedValue.value) ??
		null,
);

const hasMissingSecret = computed(() => selectedValue.value === MISSING_VALUE);

const triggerClass = computed(() => {
	if (hasMissingSecret.value) {
		return "border-red-300 text-red-900 hover:border-red-400 focus:border-red-500 focus:ring-red-500 dark:border-red-800 dark:text-red-100 dark:hover:border-red-700";
	}
	return "border-gray-300 hover:border-gray-400 focus:border-blue-500 focus:ring-blue-500 dark:border-gray-700 dark:hover:border-gray-600";
});

function toggle() {
	if (props.disabled) return;
	open.value = !open.value;
}

function select(value: string) {
	if (value === ADD_VALUE) {
		emit("add-secret");
		open.value = false;
		return;
	}
	if (value === RAW_VALUE || value === MISSING_VALUE) {
		open.value = false;
		return;
	}
	emit("update:modelValue", value);
	open.value = false;
}

function handlePointerDown(event: MouseEvent) {
	if (!open.value) return;
	if (!root.value?.contains(event.target as Node)) {
		open.value = false;
	}
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

onMounted(() => {
	document.addEventListener("mousedown", handlePointerDown);
});

onBeforeUnmount(() => {
	document.removeEventListener("mousedown", handlePointerDown);
});
</script>
