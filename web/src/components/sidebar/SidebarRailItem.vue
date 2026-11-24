<template>
  <component :is="to ? RouterLink : 'button'" ref="el" v-bind="{ ...$attrs, ...(to ? { to } : { type: 'button', disabled }) }"
    :aria-label="label" :data-active-child="activeChild || undefined"
    :class="classes" @mouseenter="show" @mouseleave="hideSoon" @focus="show" @blur="hideOnFocusOut"
    @keydown.esc="hide" @click="onClick">
    <slot />
    <span v-if="badge" data-testid="sidebar-rail-badge" :aria-label="badgeLabel"
      class="absolute -right-0.5 -top-0.5 min-w-4 rounded-full bg-green-600 px-1 text-center text-[10px] font-semibold leading-4 text-white">{{ badge }}</span>
    <span v-else-if="dot" data-testid="sidebar-rail-dot" aria-hidden="true"
      class="absolute right-1 top-1 h-2 w-2 rounded-full" :class="dot" />
    <span v-if="activeChild" data-testid="sidebar-rail-active-child" aria-hidden="true"
      class="absolute bottom-1 right-1 h-1.5 w-1.5 rounded-full bg-blue-600 dark:bg-blue-400" />
  </component>
  <!-- Fixed, so the rail's scrolling list cannot clip it, but kept next to
       its icon so Tab moves from the icon into its items. -->
  <div v-if="tip && hasItems" ref="panel" data-rail-flyout data-testid="sidebar-rail-flyout"
    class="fixed z-50 w-56 overflow-y-auto rounded-xl border border-gray-200 bg-white p-1 shadow-xl dark:border-gray-700 dark:bg-gray-900"
    :style="{ left: `${tip.x}px`, top: `${tip.top}px`, maxHeight: `calc(100vh - ${tip.top + 8}px)` }"
    @mouseenter="cancelHide" @mouseleave="hideSoon" @focusout="hideOnFocusOut" @keydown.esc="hide">
    <div role="tooltip" data-testid="sidebar-rail-tooltip"
      class="truncate px-2 pb-1 pt-1.5 text-xs font-semibold text-gray-500 dark:text-gray-400">
      {{ label }}<template v-if="badge"> · {{ badgeLabel }}</template>
    </div>
    <ul :aria-label="label">
      <li v-for="item in items" :key="item.key">
        <router-link :to="item.to" data-testid="sidebar-rail-flyout-item" :aria-current="item.active ? 'page' : undefined"
          class="block truncate rounded-lg px-2 py-1.5 text-sm transition-colors"
          :class="item.active
            ? 'bg-gray-100 font-semibold text-gray-950 dark:bg-gray-800 dark:text-white'
            : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-800 dark:hover:text-white'"
          @click="hide">{{ item.label }}</router-link>
      </li>
    </ul>
  </div>
  <Teleport v-else to="body">
    <div v-if="tip" role="tooltip" data-testid="sidebar-rail-tooltip"
      class="pointer-events-none fixed z-50 -translate-y-1/2 whitespace-nowrap rounded-md bg-gray-900 px-2 py-1 text-xs font-medium text-white shadow-lg dark:bg-gray-700"
      :style="{ left: `${tip.x}px`, top: `${tip.y}px` }">
      {{ label }}<template v-if="badge"> · {{ badgeLabel }}</template>
    </div>
  </Teleport>
</template>

<script lang="ts">
// Only one icon's tooltip or flyout shows at a time.
let closeOpen: (() => void) | null = null;

/** One sub-item in a rail icon's flyout. */
export interface RailFlyoutItem {
	key: string;
	label: string;
	to: string;
	active: boolean;
}
</script>

<script setup lang="ts">
// One icon of the collapsed sidebar rail: a link or button with its name in
// a tooltip beside it, and an optional count badge or status dot. The
// tooltip renders on the body so the rail's scrolling list cannot clip it.
// An icon whose page has sub-items lists them in a flyout instead, on hover
// or keyboard focus, and marks itself while one of them is open (#1036).
import { computed, onBeforeUnmount, ref } from "vue";
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
  /** The page's sub-items, listed in a flyout beside the icon. */
  items?: RailFlyoutItem[];
}>();
const emit = defineEmits<{ click: [] }>();
// The tooltip is a second root, so attributes such as data-testid go on the
// link or button explicitly.
defineOptions({ inheritAttrs: false });

const el = ref<{ $el?: HTMLElement } | HTMLElement | null>(null);
const panel = ref<HTMLElement | null>(null);
const tip = ref<{ x: number; y: number; top: number } | null>(null);
const HIDE_DELAY = 150;
let hideTimer: ReturnType<typeof setTimeout> | undefined;

const hasItems = computed(() => !!props.items?.length);
const activeChild = computed(() => !!props.items?.some((i) => i.active));

const badgeLabel = computed(
  () => `${props.badge} running ${props.badge === 1 ? "job" : "jobs"}`,
);

function node(): HTMLElement | undefined {
  return el.value instanceof HTMLElement ? el.value : el.value?.$el;
}

function show() {
  cancelHide();
  const n = node();
  if (!n) return;
  if (closeOpen && closeOpen !== hide) closeOpen();
  closeOpen = hide;
  const rect = n.getBoundingClientRect();
  tip.value = {
    x: rect.right + 8,
    y: rect.top + rect.height / 2,
    top: Math.max(8, rect.top - 4),
  };
}

function cancelHide() {
  clearTimeout(hideTimer);
}

function hide() {
  cancelHide();
  tip.value = null;
  if (closeOpen === hide) closeOpen = null;
}

// A flyout waits a moment, so the pointer can cross from the icon onto it.
function hideSoon() {
  if (!hasItems.value) return hide();
  cancelHide();
  hideTimer = setTimeout(hide, HIDE_DELAY);
}

// Tabbing between the icon and its flyout keeps the flyout open.
function hideOnFocusOut(e: FocusEvent) {
  const next = e.relatedTarget as Node | null;
  if (next && (node()?.contains(next) || panel.value?.contains(next))) return;
  hide();
}

function onClick() {
  hide();
  if (!props.to && !props.disabled) emit("click");
}

onBeforeUnmount(hide);

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
