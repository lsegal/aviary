<template>
	<!-- A paused scheduled task, the same amber as a paused job on the board. -->
	<span data-testid="paused-chip" title="Paused"
		class="inline-flex shrink-0 items-center gap-1 rounded-full bg-amber-50 font-semibold text-amber-800 ring-1 ring-inset ring-amber-200 dark:bg-amber-950/50 dark:text-amber-300 dark:ring-amber-900"
		:class="size === 'md' ? 'px-3 py-1 text-sm' : 'px-2 py-0.5 text-xs'">
		<PauseIcon aria-hidden="true" :class="size === 'md' ? 'h-4 w-4' : 'h-3.5 w-3.5'" />
		Paused
	</span>
</template>

<script setup lang="ts">
import PauseIcon from "@heroicons/vue/20/solid/PauseIcon";

withDefaults(defineProps<{ size?: "sm" | "md" }>(), { size: "sm" });
</script>
