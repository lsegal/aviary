<template>
  <span data-testid="project-loading-badge" title="Checking GitHub" aria-label="Checking GitHub"
    class="inline-flex shrink-0 items-center text-gray-400 dark:text-gray-500">
    <ArrowPathIcon aria-hidden="true" class="h-3.5 w-3.5 animate-spin" />
  </span>
</template>

<script setup lang="ts">
// A project just added, whose issue count and jobs Code Loop has not read
// from GitHub yet.
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
</script>
