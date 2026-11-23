<template>
	<div class="min-w-0 rounded-xl border px-4 py-3" :class="tone ? STAT_TONES[tone] : ''">
		<p class="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide opacity-80">
			<span v-if="pulse" class="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-current" />
			{{ label }}
		</p>
		<p class="truncate text-2xl font-bold">{{ value }}</p>
		<p v-if="caption" class="truncate text-[11px] opacity-70">{{ caption }}</p>
	</div>
</template>

<script setup lang="ts">
import { STAT_TONES, type StatTone } from "./statTones";

// A summary number on an overview page, such as the Scheduled Tasks and
// Channels overviews: its label, its value and a short caption.

defineProps<{
	label: string;
	value: number | string;
	caption?: string;
	/** Pulses a dot by the label, for live counts such as running jobs. */
	pulse?: boolean;
	tone?: StatTone;
}>();
</script>
