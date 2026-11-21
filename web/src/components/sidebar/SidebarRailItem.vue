<template>
  <component :is="to ? RouterLink : 'button'" ref="el" v-bind="{ ...$attrs, ...(to ? { to } : { type: 'button', disabled }) }"
    :aria-label="label"
    :class="classes" @mouseenter="show" @mouseleave="hide" @focus="show" @blur="hide" @click="onClick">
    <slot />
    <span v-if="badge" data-testid="sidebar-rail-badge" :aria-label="badgeLabel"
      class="absolute -right-0.5 -top-0.5 min-w-4 rounded-full bg-green-600 px-1 text-center text-[10px] font-semibold leading-4 text-white">{{ badge }}</span>
    <span v-else-if="dot" data-testid="sidebar-rail-dot" aria-hidden="true"
      class="absolute right-1 top-1 h-2 w-2 rounded-full" :class="dot" />
  </component>
  <Teleport to="body">
    <div v-if="tip" role="tooltip" data-testid="sidebar-rail-tooltip"
      class="pointer-events-none fixed z-50 -translate-y-1/2 whitespace-nowrap rounded-md bg-gray-900 px-2 py-1 text-xs font-medium text-white shadow-lg dark:bg-gray-700"
      :style="{ left: `${tip.x}px`, top: `${tip.y}px` }">
      {{ label }}<template v-if="badge"> · {{ badgeLabel }}</template>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
// One icon of the collapsed sidebar rail: a link or button with its name in
// a tooltip beside it, and an optional count badge or status dot. The
// tooltip renders on the body so the rail's scrolling list cannot clip it.
import { computed, ref } from "vue";
import { RouterLink } from "vue-router";

const props = defineProps<{
	label: string;
	to?: string;
	active?: boolean;
	/** Running jobs, shown as a count on the icon when any. */
	badge?: number;
	/** A status dot's color classes, shown when there is no badge. */
	dot?: string;
	disabled?: boolean;
	tone?: "danger";
}>();
const emit = defineEmits<{ click: [] }>();
// The tooltip is a second root, so attributes such as data-testid go on the
// link or button explicitly.
defineOptions({ inheritAttrs: false });

const el = ref<{ $el?: HTMLElement } | HTMLElement | null>(null);
const tip = ref<{ x: number; y: number } | null>(null);

const badgeLabel = computed(
	() => `${props.badge} running ${props.badge === 1 ? "job" : "jobs"}`,
);

function show() {
	const node = el.value instanceof HTMLElement ? el.value : el.value?.$el;
	if (!node) return;
	const rect = node.getBoundingClientRect();
	tip.value = { x: rect.right + 8, y: rect.top + rect.height / 2 };
}

function hide() {
	tip.value = null;
}

function onClick() {
	hide();
	if (!props.to && !props.disabled) emit("click");
}

const classes = computed(() => {
	const base =
		"relative inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-xl transition-colors disabled:opacity-50";
	if (props.active)
		return `${base} bg-white text-gray-950 shadow-sm dark:bg-gray-800 dark:text-white`;
	if (props.tone === "danger")
		return `${base} text-gray-500 hover:bg-red-50 hover:text-red-500 dark:text-gray-400 dark:hover:bg-red-950/40 dark:hover:text-red-400`;
	return `${base} text-gray-500 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white`;
});
</script>
