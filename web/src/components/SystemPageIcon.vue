<template>
  <span v-if="identity && tile" aria-hidden="true" data-testid="system-page-icon"
    class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg" :class="identity.tileClass">
    <component :is="identity.icon" class="h-5 w-5" :class="identity.iconClass" />
  </span>
  <span v-else-if="identity" aria-hidden="true" data-testid="system-page-icon" class="inline-flex shrink-0">
    <component :is="identity.icon" class="h-full w-full" :class="identity.iconClass" />
  </span>
</template>

<script setup lang="ts">
// A System page's icon in its accent color, bare for navigation or in a soft
// tile for cards and page headers. Always decorative: the page's visible
// title names it.
import { computed } from "vue";
import { systemPageIdentity } from "../lib/systemNav";

const props = defineProps<{ pageKey: string; tile?: boolean }>();

const identity = computed(() => systemPageIdentity(props.pageKey));
</script>
