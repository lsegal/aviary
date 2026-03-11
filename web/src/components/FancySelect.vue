<template>
  <div ref="root" class="relative w-full">
    <button
      :id="id"
      type="button"
      :disabled="disabled"
      :aria-expanded="open ? 'true' : 'false'"
      aria-haspopup="listbox"
      class="flex w-full items-center justify-between rounded-lg border border-gray-300 bg-white px-3 py-2 text-left transition hover:border-gray-400 focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:cursor-not-allowed disabled:opacity-60 dark:border-gray-700 dark:bg-gray-800 dark:hover:border-gray-600"
      @click="toggle"
    >
      <span class="min-w-0">
        <span class="block truncate text-sm font-medium text-gray-900 dark:text-white">
          {{ selectedOption?.label ?? placeholder ?? "Select an option" }}
        </span>
      </span>
      <svg
        class="ml-3 h-4 w-4 shrink-0 text-gray-400 transition-transform dark:text-gray-500"
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
          :aria-selected="option.value === modelValue ? 'true' : 'false'"
          class="w-full rounded-lg px-3 py-2 text-left transition"
          :class="
            option.value === modelValue
              ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
              : 'text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'
          "
          @click="select(option.value)"
        >
          <span class="block text-sm font-medium">{{ option.label }}</span>
          <span
            v-if="option.caption"
            class="mt-0.5 block text-xs"
            :class="
              option.value === modelValue
                ? 'text-blue-500 dark:text-blue-400'
                : 'text-gray-400 dark:text-gray-500'
            "
          >
            {{ option.caption }}
          </span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";

interface FancySelectOption {
	value: string;
	label: string;
	caption?: string;
}

const props = defineProps<{
	id?: string;
	modelValue: string;
	options: FancySelectOption[];
	placeholder?: string;
	disabled?: boolean;
}>();

const emit = defineEmits<(event: "update:modelValue", value: string) => void>();

const root = ref<HTMLElement | null>(null);
const open = ref(false);

const selectedOption = computed(
	() =>
		props.options.find((option) => option.value === props.modelValue) ?? null,
);

function toggle() {
	if (props.disabled) return;
	open.value = !open.value;
}

function select(value: string) {
	emit("update:modelValue", value);
	open.value = false;
}

function handlePointerDown(event: MouseEvent) {
	if (!open.value) return;
	if (!root.value?.contains(event.target as Node)) {
		open.value = false;
	}
}

onMounted(() => {
	document.addEventListener("mousedown", handlePointerDown);
});

onBeforeUnmount(() => {
	document.removeEventListener("mousedown", handlePointerDown);
});
</script>
