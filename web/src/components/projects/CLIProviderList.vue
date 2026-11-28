<template>
	<section class="flex max-w-full flex-wrap items-center gap-2 text-xs" :class="{ 'flex-col items-start': stacked }"
		aria-label="CLI providers" :data-testid="testid">
		<span class="font-medium text-gray-600 dark:text-gray-300">CLI providers</span>
		<span v-for="cli in clis" :key="cli.name" data-testid="cli-provider"
			class="inline-flex max-w-full flex-wrap items-center gap-x-1.5 rounded-full border border-gray-200 px-2.5 py-1 dark:border-gray-700">
			<span class="h-1.5 w-1.5 rounded-full" :class="cli.signedIn ? 'bg-green-500' : 'bg-gray-400'" />
			<b class="text-gray-800 dark:text-gray-100">{{ cli.name }}</b>
			<span v-if="cli.models.length" class="text-gray-600 dark:text-gray-300">{{ cli.models.join(", ") }}</span>
			<span class="text-gray-500">{{ cli.quota || "no quota reported" }}</span>
		</span>
	</section>
</template>

<script setup lang="ts">
// The Code Loop board's CLI providers: each one's status dot, name, the models
// its jobs run on and its quota. It shows as a row under the stats on a wide
// screen, and stacked in the header's details popover on a phone (#1580).
import type { CLIStatus } from "../../lib/projects";

defineProps<{
	clis: readonly CLIStatus[];
	/** One provider per line, for the details popover and "…" menu. */
	stacked?: boolean;
	testid?: string;
}>();
</script>
