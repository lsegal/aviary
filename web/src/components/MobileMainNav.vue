<template>
  <nav ref="row" class="relative flex min-w-0 flex-nowrap items-center gap-2" aria-label="Main" data-testid="mobile-main-nav">
    <router-link v-for="item in visibleItems" :key="item.key" :to="item.to" :data-testid="item.testid"
      :class="pillClass(item.active)">
      {{ item.label }}<ProjectRunningBadge :running="item.running ?? 0" class="ml-1.5" />
    </router-link>
    <div v-if="hiddenItems.length" ref="more" class="relative shrink-0">
      <button ref="moreButton" type="button" aria-label="More" title="More" aria-haspopup="menu"
        :aria-expanded="open" data-testid="mobile-nav-more" :class="[pillClass(false), 'relative']"
        @click="toggle()" @keydown="onButtonKey">
        <span aria-hidden="true">…</span>
        <span v-if="hiddenBadge" data-testid="mobile-nav-more-dot"
          class="absolute -right-0.5 -top-0.5 h-2.5 w-2.5 rounded-full border-2 border-gray-50 bg-green-600 dark:border-gray-900" />
      </button>
      <div v-if="open" ref="menu" role="menu" aria-label="More pages" data-testid="mobile-nav-menu"
        class="absolute right-0 top-full z-30 mt-1 min-w-48 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-gray-700 dark:bg-gray-800"
        @keydown="onMenuKey">
        <router-link v-for="item in hiddenItems" :key="item.key" :to="item.to" role="menuitem" tabindex="-1"
          :data-testid="item.testid" :data-active="item.active || undefined"
          :class="menuItemClass(item.active)" @click="close(false)">
          <span class="min-w-0 flex-1 truncate">{{ item.label }}</span>
          <ProjectRunningBadge :running="item.running ?? 0" />
          <CheckIcon v-if="item.active" aria-label="Current page" class="h-4 w-4 shrink-0" />
        </router-link>
      </div>
    </div>

    <!-- Every pill at its natural width, measured to decide what fits. Its
         text comes from CSS so it never matches a text search of the page. -->
    <div aria-hidden="true" class="pointer-events-none invisible absolute left-0 top-0 h-0 w-0 overflow-hidden">
      <div ref="measure" class="flex w-max flex-nowrap gap-2">
        <span v-for="item in items" :key="item.key" :data-key="item.key" :data-label="item.label"
          :data-count="item.running || undefined" :class="[pillClass(item.active), measureClass(item)]" />
        <span data-key="__more" data-label="…" :class="[pillClass(false), 'before:content-[attr(data-label)]']" />
      </div>
    </div>
  </nav>
</template>

<script setup lang="ts">
import CheckIcon from "@heroicons/vue/24/outline/CheckIcon";
import {
	computed,
	nextTick,
	onBeforeUnmount,
	onMounted,
	ref,
	watch,
} from "vue";
import type { RouteLocationRaw } from "vue-router";
import ProjectRunningBadge from "./projects/ProjectRunningBadge.vue";

export interface MobileNavItem {
	key: string;
	label: string;
	to: RouteLocationRaw;
	active: boolean;
	running?: number;
	testid?: string;
}

// Items arrive in display order, which is also their priority after the
// active one: the row shows the longest prefix that fits on one line and the
// rest go behind "…".
const props = defineProps<{ items: MobileNavItem[] }>();

const GAP = 8;

const row = ref<HTMLElement | null>(null);
const measure = ref<HTMLElement | null>(null);
const more = ref<HTMLElement | null>(null);
const moreButton = ref<HTMLButtonElement | null>(null);
const menu = ref<HTMLElement | null>(null);
const shown = ref<Set<string> | null>(null);
const open = ref(false);

const visibleItems = computed(() =>
	shown.value
		? props.items.filter((i) => shown.value?.has(i.key))
		: props.items,
);
const hiddenItems = computed(() =>
	shown.value ? props.items.filter((i) => !shown.value?.has(i.key)) : [],
);
const hiddenBadge = computed(() =>
	hiddenItems.value.some((i) => (i.running ?? 0) > 0),
);

function recompute() {
	const width = row.value?.clientWidth ?? 0;
	const pills = measure.value;
	if (!width || !pills) return;
	const widths = new Map<string, number>();
	for (const el of pills.children) {
		const key = (el as HTMLElement).dataset.key ?? "";
		widths.set(key, el.getBoundingClientRect().width);
	}
	const sum = (keys: string[]) =>
		keys.reduce((n, k) => n + (widths.get(k) ?? 0), 0) +
		GAP * Math.max(0, keys.length - 1);
	const all = props.items.map((i) => i.key);
	if (sum(all) <= width) {
		shown.value = new Set(all);
		return;
	}
	const active = props.items.filter((i) => i.active).map((i) => i.key);
	const order = [...active, ...all.filter((k) => !active.includes(k))];
	const keep: string[] = [];
	for (const key of order) {
		if (sum([...keep, key, "__more"]) > width && !active.includes(key)) break;
		keep.push(key);
	}
	shown.value = new Set(keep);
}

let observer: ResizeObserver | undefined;
onMounted(() => {
	observer = new ResizeObserver(() => recompute());
	if (row.value) observer.observe(row.value);
	if (measure.value) observer.observe(measure.value);
	recompute();
	document.addEventListener("pointerdown", onOutside, true);
});
onBeforeUnmount(() => {
	observer?.disconnect();
	document.removeEventListener("pointerdown", onOutside, true);
});
watch(
	() => props.items,
	async () => {
		await nextTick();
		recompute();
	},
	{ deep: true },
);
watch(hiddenItems, (items) => {
	if (!items.length) open.value = false;
});

function menuItems(): HTMLElement[] {
	return [
		...(menu.value?.querySelectorAll<HTMLElement>("[role=menuitem]") ?? []),
	];
}

async function openMenu(focus: "first" | "last" | "active" | null) {
	open.value = true;
	if (!focus) return;
	await nextTick();
	const els = menuItems();
	const target =
		focus === "last"
			? els[els.length - 1]
			: focus === "active"
				? (els.find((el) => el.dataset.active) ?? els[0])
				: els[0];
	target?.focus();
}

function close(refocus: boolean) {
	open.value = false;
	if (refocus) moreButton.value?.focus();
}

function toggle() {
	if (open.value) close(false);
	else void openMenu("active");
}

function onButtonKey(e: KeyboardEvent) {
	if (e.key === "ArrowDown") {
		e.preventDefault();
		void openMenu("first");
	} else if (e.key === "ArrowUp") {
		e.preventDefault();
		void openMenu("last");
	} else if (e.key === "Escape" && open.value) {
		e.preventDefault();
		close(true);
	}
}

function onMenuKey(e: KeyboardEvent) {
	const els = menuItems();
	const i = els.indexOf(document.activeElement as HTMLElement);
	const focusAt = (n: number) => els[(n + els.length) % els.length]?.focus();
	switch (e.key) {
		case "ArrowDown":
			e.preventDefault();
			focusAt(i + 1);
			break;
		case "ArrowUp":
			e.preventDefault();
			focusAt(i - 1);
			break;
		case "Home":
			e.preventDefault();
			focusAt(0);
			break;
		case "End":
			e.preventDefault();
			focusAt(els.length - 1);
			break;
		case "Escape":
			e.preventDefault();
			close(true);
			break;
		case "Tab":
			close(false);
			break;
		case " ":
			e.preventDefault();
			(document.activeElement as HTMLElement | null)?.click();
			break;
	}
}

function onOutside(e: PointerEvent) {
	if (open.value && !more.value?.contains(e.target as Node)) close(false);
}

function pillClass(active: boolean): string {
	const base =
		"inline-flex shrink-0 items-center whitespace-nowrap rounded-xl border px-3 py-1.5 text-sm font-medium transition-colors";
	return active
		? `${base} border-gray-300 bg-white text-gray-950 shadow-sm dark:border-gray-600 dark:bg-gray-800 dark:text-white`
		: `${base} border-gray-200 bg-white text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white`;
}

function measureClass(item: MobileNavItem): string {
	const label = "before:content-[attr(data-label)]";
	return (item.running ?? 0) > 0
		? `${label} after:ml-1.5 after:px-1.5 after:text-xs after:font-semibold after:content-[attr(data-count)]`
		: label;
}

function menuItemClass(active: boolean): string {
	const base =
		"flex items-center gap-2 rounded-lg px-3 py-2 text-sm outline-none transition-colors focus:bg-gray-100 dark:focus:bg-gray-700";
	return active
		? `${base} font-semibold text-gray-950 dark:text-white`
		: `${base} text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white`;
}
</script>
