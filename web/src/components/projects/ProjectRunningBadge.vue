<template>
  <span v-if="running > 0" data-testid="project-running-badge" :title="label" :aria-label="label"
    class="shrink-0 rounded-full bg-gray-200 px-1.5 text-xs font-semibold text-gray-700 dark:bg-gray-700 dark:text-gray-200">{{ running }}</span>
</template>

<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{ running: number }>();

const label = computed(
	() => `${props.running} running ${props.running === 1 ? "job" : "jobs"}`,
);
</script>
