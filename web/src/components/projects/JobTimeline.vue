<template>
	<ol class="space-y-3" aria-label="Activity">
		<li v-if="!events.length" class="py-8 text-center text-sm text-gray-400">No activity yet.</li>
		<li v-for="(e, i) in events" :key="i" :data-event="e.kind">
			<div v-if="e.kind === 'message'"
				class="prose max-w-2xl rounded-xl bg-gray-100 px-4 py-2 text-gray-900 dark:prose-invert dark:bg-gray-800 dark:text-gray-100"
				v-html="renderMarkdown(e.text)" />

			<details v-else-if="e.kind === 'reasoning'" class="max-w-2xl text-sm text-gray-500 dark:text-gray-400">
				<summary class="cursor-pointer select-none italic">Thinking</summary>
				<p class="mt-1 whitespace-pre-wrap border-l-2 border-gray-200 pl-3 dark:border-gray-700">{{ e.text }}</p>
			</details>

			<ChatToolCall v-else-if="e.kind === 'tool_call'" :name="e.name" :args="e.input" :result="e.result?.output"
				:error="e.result?.error ? e.result.output || 'Error' : undefined" :images="e.result?.images"
				:pending="!e.result" />

			<details v-else-if="e.kind === 'command'" class="group max-w-full">
				<summary class="flex cursor-pointer list-none items-baseline gap-2 font-mono text-xs text-gray-600 dark:text-gray-300">
					<span class="text-gray-400">&gt;_</span>
					<span class="min-w-0 truncate">{{ e.command }}</span>
					<span v-if="e.running" class="text-blue-500">running…</span>
					<span v-else-if="e.exitCode === 0" class="text-green-600">exit 0</span>
					<span v-else-if="e.exitCode !== null" class="text-red-600">exit {{ e.exitCode }}</span>
				</summary>
				<pre class="mt-1.5 max-h-64 overflow-auto rounded-lg bg-slate-950 p-3 font-mono text-xs text-slate-200">{{ e.output || "No output" }}</pre>
			</details>

			<details v-else-if="e.kind === 'file_change'" class="max-w-full">
				<summary class="flex cursor-pointer list-none items-baseline gap-2 font-mono text-xs text-gray-600 dark:text-gray-300">
					<span class="text-gray-400">✎</span>
					<span class="min-w-0 truncate">edit {{ e.path }}</span>
					<span class="text-green-600">+{{ diffStat(e.diff).added }}</span>
					<span class="text-red-600">-{{ diffStat(e.diff).removed }}</span>
				</summary>
				<pre class="mt-1.5 max-h-80 overflow-auto rounded-lg border border-gray-200 font-mono text-xs dark:border-gray-800"><span
					v-for="(l, j) in diffLines(e.diff)" :key="j" class="block px-3" :class="DIFF_CLASS[l.kind]">{{ l.text || " " }}</span></pre>
			</details>

			<div v-else-if="e.kind === 'plan'" class="max-w-2xl rounded-xl border border-gray-200 px-4 py-3 text-sm dark:border-gray-800">
				<p class="mb-1 text-xs font-semibold uppercase tracking-wide text-gray-400">Plan</p>
				<ul class="space-y-0.5">
					<li v-for="(item, j) in e.items" :key="j" class="flex gap-2"
						:class="item.status === 'completed' ? 'text-gray-400 line-through' : 'text-gray-800 dark:text-gray-200'">
						<span aria-hidden="true">{{ item.status === "completed" ? "☑" : item.status === "in_progress" ? "▸" : "☐" }}</span>
						<span>{{ item.text }}</span>
					</li>
				</ul>
			</div>

			<p v-else-if="e.kind === 'usage'" class="text-center text-xs text-gray-400">
				{{ e.inputTokens.toLocaleString() }} in · {{ e.outputTokens.toLocaleString() }} out<template v-if="e.cost"> · ${{ e.cost.toFixed(2) }}</template>
			</p>

			<div v-else-if="e.kind === 'comment_relay'"
				class="max-w-2xl rounded-xl border border-gray-200 bg-white px-4 py-3 text-sm dark:border-gray-800 dark:bg-gray-900">
				<p class="text-gray-600 dark:text-gray-300">
					<b class="text-gray-900 dark:text-white">{{ e.author }}</b> commented
					· sent to the job
					<a v-if="e.url" :href="e.url" target="_blank" rel="noreferrer" class="text-blue-600 hover:underline">view</a>
				</p>
				<div class="prose prose-sm mt-1 max-w-none dark:prose-invert" v-html="renderMarkdown(e.body)" />
			</div>

			<p v-else-if="e.kind === 'system'" class="text-center text-xs text-gray-500 dark:text-gray-400">
				{{ e.text }}<template v-if="e.time"> · {{ formatTime(e.time) }}</template>
			</p>
		</li>
	</ol>
</template>

<script setup lang="ts">
import { renderMarkdown } from "../../lib/markdown";
import { diffLines, diffStat, type JobEvent } from "../../lib/projects";
import ChatToolCall from "../chat/ChatToolCall.vue";

defineProps<{ events: JobEvent[] }>();

const DIFF_CLASS = {
	add: "bg-green-50 text-green-800 dark:bg-green-950/40 dark:text-green-300",
	del: "bg-red-50 text-red-800 dark:bg-red-950/40 dark:text-red-300",
	hunk: "bg-blue-50 text-blue-700 dark:bg-blue-950/40 dark:text-blue-300",
	ctx: "text-gray-600 dark:text-gray-400",
};

function formatTime(iso: string): string {
	const d = new Date(iso);
	return Number.isNaN(d.getTime())
		? ""
		: d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
</script>
