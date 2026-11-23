<template>
  <!-- One line that scrolls sideways instead of wrapping, like the Code Loop
       chip row (#1003): swipe it on touch, with a thin scrollbar and a fade
       on each edge that has more pills past it. -->
  <nav ref="row" aria-label="Main" data-testid="mobile-main-nav"
    class="flex min-w-0 flex-nowrap items-center gap-2 overflow-x-auto overscroll-x-contain pb-1.5 [scrollbar-width:thin]"
    :style="fadeStyle" @scroll.passive="measure">
    <router-link v-for="item in items" :key="item.key" :to="item.to" :data-testid="item.testid"
      :data-key="item.key" :data-active="item.active || undefined" :aria-current="item.active ? 'page' : undefined"
      :class="pillClass(item.active)">
      {{ item.label }}
      <ProjectRunningBadge :running="item.running ?? 0" class="ml-1.5" />
    </router-link>
  </nav>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import type { RouteLocationRaw } from "vue-router";
import { useScrollFade } from "../composables/useScrollFade";
import ProjectRunningBadge from "./projects/ProjectRunningBadge.vue";

export interface MobileNavItem {
	key: string;
	label: string;
	to: RouteLocationRaw;
	active: boolean;
	running?: number;
	testid?: string;
}

// Items arrive in the user's order (#1003). Every item stays in the row;
// the row scrolls instead of moving any behind a menu.
const props = defineProps<{ items: MobileNavItem[] }>();

const row = ref<HTMLElement | null>(null);
const { fadeStyle, measure, observe } = useScrollFade(row);

/** Scrolls the row, not the page, just enough to show the active pill. */
function revealActive() {
	const el = row.value;
	const pill = el?.querySelector<HTMLElement>("[data-active]");
	if (!el || !pill) return;
	const box = pill.getBoundingClientRect();
	const left = box.left - el.getBoundingClientRect().left + el.scrollLeft;
	const right = left + box.width;
	if (left < el.scrollLeft) el.scrollLeft = left;
	else if (right > el.scrollLeft + el.clientWidth)
		el.scrollLeft = right - el.clientWidth;
	measure();
}

onMounted(revealActive);
const activeKey = computed(() => props.items.find((i) => i.active)?.key);
watch(
	() => props.items.map((i) => i.key).join("\n"),
	async () => {
		await nextTick();
		observe();
		revealActive();
	},
);
watch(activeKey, async () => {
	await nextTick();
	revealActive();
});

function pillClass(active: boolean): string {
	const base =
		"inline-flex shrink-0 items-center whitespace-nowrap rounded-xl border px-3 py-1.5 text-sm font-medium transition-colors";
	return active
		? `${base} border-gray-300 bg-white text-gray-950 shadow-sm dark:border-gray-600 dark:bg-gray-800 dark:text-white`
		: `${base} border-gray-200 bg-white text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white`;
}
</script>
