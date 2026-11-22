<template>
  <span v-if="identity && tile" aria-hidden="true" data-testid="system-page-icon"
    class="inline-flex shrink-0 items-center justify-center rounded-lg"
    :class="[identity.tileClass, compactOnMobile ? 'h-7 w-7 sm:h-9 sm:w-9' : 'h-9 w-9']">
    <component :is="identity.icon" :class="[identity.iconClass, compactOnMobile ? 'h-4 w-4 sm:h-5 sm:w-5' : 'h-5 w-5']" />
  </span>
  <span v-else-if="identity" aria-hidden="true" data-testid="system-page-icon" class="inline-flex shrink-0">
    <component :is="identity.icon" class="h-full w-full" :class="identity.iconClass" />
  </span>
</template>

<script setup lang="ts">
// A System page's icon in its accent color, bare for navigation or in a soft
// tile for cards and page headers. Always decorative: the page's visible
// title names it. compactOnMobile shrinks the tile below the sm breakpoint
// for dense card grids, leaving page headers at full size.
import { computed } from "vue";
import { systemPageIdentity } from "../lib/systemNav";

const props = defineProps<{
	pageKey: string;
	tile?: boolean;
	compactOnMobile?: boolean;
}>();

const identity = computed(() => systemPageIdentity(props.pageKey));
</script>
