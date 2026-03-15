<template>
  <div class="relative w-full">
    <!-- Selection Area -->
    <div
      :class="[
        'flex items-center gap-1.5 p-1.5 min-h-[38px] rounded-lg border border-gray-300 bg-white focus-within:border-blue-500 focus-within:ring-1 focus-within:ring-blue-500 dark:border-gray-700 dark:bg-gray-800 transition-all cursor-text',
        multiple ? 'flex-wrap' : 'flex-nowrap overflow-hidden'
      ]"
      @mousedown="onContainerMouseDown"
    >
      <!-- Multiple selection chips -->
      <template v-if="multiple && Array.isArray(modelValue)">
        <div v-for="(val, idx) in modelValue" :key="idx"
          class="flex items-center gap-1 px-2 py-0.5 rounded-md bg-blue-50 text-blue-700 text-xs font-medium border border-blue-100 dark:bg-blue-900/30 dark:text-blue-300 dark:border-blue-800 shrink-0"
        >
          <span class="truncate max-w-[150px]">{{ val }}</span>
          <button type="button" @click.stop="remove(idx)" class="hover:text-blue-900 dark:hover:text-blue-100 transition-colors">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" viewBox="0 0 20 20" fill="currentColor">
              <path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
            </svg>
          </button>
        </div>
      </template>

      <!-- Single selection chip -->
      <template v-else-if="singleValue">
        <div class="flex shrink-0 items-center gap-1 px-2 py-0.5 rounded-md bg-gray-100 text-gray-800 text-xs font-medium border border-gray-200 dark:bg-gray-700 dark:text-gray-200 dark:border-gray-600">
          <span class="truncate max-w-[200px]">{{ singleValue }}</span>
          <button type="button" @click.stop="emit('update:modelValue', '')" class="hover:text-gray-900 dark:hover:text-white transition-colors">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" viewBox="0 0 20 20" fill="currentColor">
              <path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd" />
            </svg>
          </button>
        </div>
      </template>

      <input
        ref="input"
        v-model="query"
        type="text"
        :placeholder="(multiple || !modelValue) ? placeholder : ''"
        class="flex-1 min-w-[60px] bg-transparent border-none outline-none text-sm text-gray-900 dark:text-white placeholder-gray-400"
        @focus="onFocus"
        @input="onInput"
        @click="onClick"
        @keydown.down.prevent="move(1)"
        @keydown.up.prevent="move(-1)"
        @keydown.enter.prevent="selectActive()"
        @keydown.backspace="handleBackspace"
        @keydown.esc="isOpen = false"
        @blur="onBlur"
      />
    </div>

    <!-- Dropdown -->
    <div v-if="isOpen"
      class="absolute z-50 mt-1 w-full max-h-72 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-xl dark:border-gray-700 dark:bg-gray-900"
    >
      <div v-if="filteredOptions.length">
        <div
          v-for="(opt, idx) in filteredOptions"
          :key="opt"
          :class="[
            'cursor-pointer px-4 py-2 text-sm transition-colors',
            activeIndex === idx ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300' : 'text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800'
          ]"
          @mousedown.prevent="select(opt)"
        >
          <div class="min-w-0">
            <div class="truncate font-medium">{{ opt }}</div>
            <div class="mt-1 flex items-center gap-2 text-[11px] text-gray-500 dark:text-gray-400">
              <div v-if="optionDetail(opt)" class="min-w-0 flex-1 truncate">
                {{ optionDetail(opt) }}
              </div>
              <span
                v-if="isKnownModel(opt)"
                class="shrink-0 rounded-full px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-[0.12em]"
                :class="
                  lookupModel(opt)?.supports_image_input
                    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                    : 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300'
                "
              >
                {{ modelSupportLabel(opt) }}
              </span>
            </div>
          </div>
        </div>
      </div>
      <div v-else class="px-4 py-2 text-sm text-gray-400 italic">
        {{ props.emptyText ?? "No matching options found" }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
	lookupModel,
	modelDetailLabel,
	modelSupportLabel,
	SUPPORTED_MODELS,
} from "../constants/models";

const props = defineProps<{
	modelValue: string | string[];
	multiple?: boolean;
	placeholder?: string;
	options?: string[];
	emptyText?: string;
}>();

const emit = defineEmits(["update:modelValue"]);

const input = ref<HTMLInputElement | null>(null);
const query = ref("");
const isOpen = ref(false);
const activeIndex = ref(0);
const singleValue = computed(() =>
	typeof props.modelValue === "string" ? props.modelValue : "",
);

const filteredOptions = computed(() => {
	const q = query.value.toLowerCase().trim();
	const options = props.options ?? SUPPORTED_MODELS;
	return options.filter((m) => {
		if (props.multiple && Array.isArray(props.modelValue)) {
			if (props.modelValue.includes(m)) return false;
		}
		if (!q) return true;
		return (
			m.toLowerCase().includes(q) ||
			optionDetail(m).toLowerCase().includes(q) ||
			modelSupportLabel(m).toLowerCase().includes(q)
		);
	});
});

function isKnownModel(opt: string): boolean {
	return lookupModel(opt) !== undefined;
}

function optionDetail(opt: string): string {
	if (!isKnownModel(opt)) return "";
	return modelDetailLabel(opt);
}

watch(query, () => {
	activeIndex.value = 0;
	isOpen.value = true;
});

function move(delta: number) {
	const len = filteredOptions.value.length;
	if (!len) return;
	activeIndex.value = (activeIndex.value + delta + len) % len;
}

function selectActive() {
	if (filteredOptions.value[activeIndex.value]) {
		select(filteredOptions.value[activeIndex.value]);
	}
}

function select(opt: string) {
	if (props.multiple) {
		const list = Array.isArray(props.modelValue) ? [...props.modelValue] : [];
		if (!list.includes(opt)) {
			list.push(opt);
			emit("update:modelValue", list);
		}
	} else {
		emit("update:modelValue", opt);
	}
	query.value = "";
	isOpen.value = false;
	// Blurring the input after selection makes it clearer that the action is done
	input.value?.blur();
}

function remove(idx: number) {
	if (props.multiple && Array.isArray(props.modelValue)) {
		const list = [...props.modelValue];
		list.splice(idx, 1);
		emit("update:modelValue", list);
	}
}

function handleBackspace() {
	if (!query.value && props.modelValue) {
		if (
			props.multiple &&
			Array.isArray(props.modelValue) &&
			props.modelValue.length > 0
		) {
			remove(props.modelValue.length - 1);
		} else if (!props.multiple) {
			emit("update:modelValue", "");
		}
	}
}

function onFocus() {
	isOpen.value = true;
}

function onInput() {
	isOpen.value = true;
}

function onClick() {
	isOpen.value = true;
}

function onContainerMouseDown(e: MouseEvent) {
	if (e.target !== input.value) {
		e.preventDefault();
		input.value?.focus();
	}
	isOpen.value = true;
}

function onBlur() {
	// Delay to allow mousedown to trigger first
	setTimeout(() => {
		isOpen.value = false;
		query.value = "";
	}, 150);
}
</script>
