<template>
	<details class="group inline-block max-w-full" :open="open">
		<summary :data-state="state" :title="error || undefined"
			class="inline-flex cursor-pointer list-none items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs hover:border-gray-300 hover:bg-gray-100 dark:hover:border-gray-600 dark:hover:bg-gray-800"
			:class="error
				? 'border-red-200 bg-red-50 text-red-600 dark:border-red-900 dark:bg-red-950/40 dark:text-red-400'
				: 'border-gray-200 bg-gray-50 text-gray-500 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-400'">
			<svg v-if="state === 'running'" data-testid="tool-call-spinner" xmlns="http://www.w3.org/2000/svg"
				viewBox="0 0 16 16" fill="none" class="h-3 w-3 shrink-0 animate-spin text-blue-500 motion-reduce:animate-pulse dark:text-blue-400"
				aria-hidden="true">
				<circle cx="8" cy="8" r="6" stroke="currentColor" stroke-opacity="0.25" stroke-width="2" />
				<path d="M14 8a6 6 0 0 0-6-6" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
			</svg>
			<svg v-else-if="state === 'failed'" data-testid="tool-call-warning" xmlns="http://www.w3.org/2000/svg"
				viewBox="0 0 16 16" fill="currentColor" class="h-3 w-3 shrink-0" aria-hidden="true">
				<path fill-rule="evenodd"
					d="M6.701 2.25c.577-1 2.02-1 2.598 0l5.196 9a1.5 1.5 0 0 1-1.299 2.25H2.804a1.5 1.5 0 0 1-1.3-2.25l5.197-9ZM8 5a.75.75 0 0 1 .75.75v2.5a.75.75 0 0 1-1.5 0v-2.5A.75.75 0 0 1 8 5Zm0 6.5a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z"
					clip-rule="evenodd" />
			</svg>
			<svg v-else data-testid="tool-call-check" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"
				fill="currentColor" class="h-3 w-3 shrink-0 text-green-600 dark:text-green-400" aria-hidden="true">
				<path fill-rule="evenodd"
					d="M12.416 3.376a.75.75 0 0 1 .208 1.04l-5 7.5a.75.75 0 0 1-1.154.114l-3-3a.75.75 0 0 1 1.06-1.06l2.353 2.353 4.493-6.74a.75.75 0 0 1 1.04-.207Z"
					clip-rule="evenodd" />
			</svg>
			<span>{{ name }}</span>
			<span v-if="elapsed !== undefined" data-testid="tool-call-elapsed"
				class="tabular-nums">· {{ elapsed }}s</span>
			<span class="sr-only">({{ state }})</span>
			<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"
				class="h-2.5 w-2.5 shrink-0 transition-transform group-open:rotate-180" aria-hidden="true">
				<path fill-rule="evenodd"
					d="M4.22 6.22a.75.75 0 0 1 1.06 0L8 8.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 7.28a.75.75 0 0 1 0-1.06Z"
					clip-rule="evenodd" />
			</svg>
		</summary>
		<div class="mt-1.5 rounded-lg border border-gray-200 bg-gray-50 p-3 text-xs dark:border-gray-700 dark:bg-gray-900">
			<div v-if="hasArgs">
				<p class="mb-1 font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">Arguments</p>
				<pre class="max-h-40 overflow-auto whitespace-pre-wrap break-all text-gray-700 dark:text-gray-300">{{ formattedArgs }}</pre>
			</div>
			<div v-if="result" :class="hasArgs ? 'mt-2' : ''">
				<p class="mb-1 font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">Result</p>
				<pre class="max-h-48 overflow-auto whitespace-pre-wrap break-all text-gray-700 dark:text-gray-300">{{ result }}</pre>
			</div>
			<div v-if="images?.length" class="mt-2 flex flex-wrap gap-2">
				<img v-for="src in images" :key="src" :src="src" alt="Tool result image" class="max-h-48 rounded" />
			</div>
			<div v-if="error" :class="hasArgs ? 'mt-2' : ''">
				<p class="mb-1 font-semibold uppercase tracking-wide text-red-400">Error</p>
				<pre class="whitespace-pre-wrap break-all text-red-600 dark:text-red-400">{{ error }}</pre>
			</div>
			<p v-if="!result && !error && !images?.length" class="italic text-gray-400 dark:text-gray-500">
				{{ pending ? "Running…" : "No output" }}
			</p>
		</div>
	</details>
</template>

<script setup lang="ts">
import { computed } from "vue";

const props = withDefaults(
	defineProps<{
		name: string;
		args?: unknown;
		result?: string;
		error?: string;
		images?: string[];
		pending?: boolean;
		/** Seconds the call has taken (so far, while running), once it is slow. */
		elapsed?: number;
		open?: boolean;
	}>(),
	{ pending: true },
);

const state = computed(() =>
	props.error ? "failed" : props.pending ? "running" : "succeeded",
);

const hasArgs = computed(() => {
	const a = props.args;
	if (a === undefined || a === null || a === "") return false;
	if (typeof a === "object") return Object.keys(a as object).length > 0;
	return true;
});

const formattedArgs = computed(() =>
	typeof props.args === "string"
		? props.args
		: JSON.stringify(props.args, null, 2),
);
</script>
