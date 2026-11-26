<template>
  <!-- The second row on a narrow screen: a strip of tabs that scrolls
       sideways, with an optional action ("+") pinned to the row's right edge
       outside the strip, so it never scrolls with or covers a tab (#1403).
       Each edge of the strip with more tabs past it fades out, and the
       strip shows no scrollbar (#1209). -->
  <div data-testid="mobile-sub-nav-row" class="flex min-w-0 items-center gap-1">
    <nav ref="row" data-testid="mobile-sub-nav"
      class="flex min-w-0 flex-1 flex-nowrap gap-2 overflow-x-auto overscroll-x-contain pb-1 scrollbar-none"
      :style="fadeStyle" @scroll.passive="measure">
      <slot />
    </nav>
    <div v-if="$slots.action" class="flex shrink-0 items-center pb-1">
      <slot name="action" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from "vue";
import { useScrollFade } from "../composables/useScrollFade";

// `tabs` identifies the tab set and `active` the current tab; when either
// changes the strip re-measures and scrolls the active tab (marked with
// `data-active`) into view.
const props = defineProps<{ tabs: string; active?: string }>();

const row = ref<HTMLElement | null>(null);
const { fadeStyle, measure, observe, revealActive } = useScrollFade(row);

onMounted(() => revealActive());
watch(
	() => props.tabs,
	async () => {
		await nextTick();
		observe();
		revealActive();
	},
);
watch(
	() => props.active,
	async () => {
		await nextTick();
		revealActive();
	},
);
</script>
