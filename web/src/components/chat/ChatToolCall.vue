<template>
	<details class="group inline-block max-w-full" :open="open">
		<summary
			class="inline-flex cursor-pointer list-none items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs hover:border-gray-300 hover:bg-gray-100 dark:hover:border-gray-600 dark:hover:bg-gray-800"
			:class="error
				? 'border-red-200 bg-red-50 text-red-600 dark:border-red-900 dark:bg-red-950/40 dark:text-red-400'
				: 'border-gray-200 bg-gray-50 text-gray-500 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-400'">
			<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" class="h-3 w-3 shrink-0"
				aria-hidden="true">
				<path fill-rule="evenodd"
					d="M5.433 2.304A4.492 4.492 0 0 0 3.5 6c0 1.92 1.207 3.563 2.912 4.205l-1.69 3.668-.776-.776a.75.75 0 0 0-1.06 1.06l2 2a.75.75 0 0 0 1.172-.196l2-4.34A4.492 4.492 0 0 0 8 12.5c.578 0 1.131-.109 1.64-.307l2 4.34a.75.75 0 0 0 1.172.196l2-2a.75.75 0 1 0-1.06-1.06l-.777.776-1.69-3.668A4.5 4.5 0 1 0 5.433 2.304Zm3.388 6.787A3 3 0 1 1 8 3a3 3 0 0 1 .821 6.091Z"
					clip-rule="evenodd" />
			</svg>
			<span>{{ name }}</span>
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
		open?: boolean;
	}>(),
	{ pending: true },
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
