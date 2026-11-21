<template>
	<ol class="min-w-0 space-y-1.5 text-xs" aria-label="Job output" data-testid="job-rich-output">
		<template v-for="row in rows" :key="row.key">
			<li v-if="row.kind === 'message'" data-row="message"
				class="prose prose-sm min-w-0 max-w-none text-gray-800 [overflow-wrap:anywhere] prose-pre:overflow-x-hidden prose-pre:whitespace-pre-wrap prose-table:table-fixed dark:prose-invert dark:text-gray-100 [&_p]:my-1"
				v-html="renderMarkdown(row.text)" />
			<li v-else-if="row.kind === 'note'" data-row="note" class="truncate text-gray-500 dark:text-gray-400"
				:title="row.text">{{ row.text }}</li>
			<li v-else data-row="tool" :data-call-id="row.callID || undefined" :data-status="row.status"
				class="flex min-w-0 items-center gap-2 rounded-md bg-gray-50 px-2 py-1 dark:bg-gray-800/60">
				<component :is="row.icon" aria-hidden="true" class="h-3.5 w-3.5 shrink-0 text-gray-400" />
				<span class="min-w-0 flex-1 truncate font-mono text-gray-700 dark:text-gray-200" :title="row.text">{{
					row.text }}</span>
				<span v-if="row.status === 'running'" data-testid="tool-status"
					class="flex shrink-0 items-center gap-1 rounded-full bg-amber-50 px-1.5 py-0.5 font-medium text-amber-700 dark:bg-amber-950/50 dark:text-amber-300">
					<ArrowPathIcon aria-hidden="true" class="h-3 w-3 animate-spin" />running
				</span>
				<span v-else-if="row.status === 'error'" data-testid="tool-status" :title="row.error"
					class="shrink-0 rounded-full bg-red-50 px-1.5 py-0.5 font-medium text-red-700 dark:bg-red-950/50 dark:text-red-300">
					✕ {{ row.badge || "error" }}
				</span>
				<span v-else data-testid="tool-status"
					class="shrink-0 rounded-full bg-green-50 px-1.5 py-0.5 font-medium text-green-700 dark:bg-green-950/50 dark:text-green-300">
					✓ {{ row.badge || "ok" }}
				</span>
			</li>
		</template>
	</ol>
</template>

<script setup lang="ts">
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import ChatBubbleLeftEllipsisIcon from "@heroicons/vue/24/outline/ChatBubbleLeftEllipsisIcon";
import CommandLineIcon from "@heroicons/vue/24/outline/CommandLineIcon";
import PencilSquareIcon from "@heroicons/vue/24/outline/PencilSquareIcon";
import WrenchScrewdriverIcon from "@heroicons/vue/24/outline/WrenchScrewdriverIcon";
import { type Component, computed } from "vue";
import { renderMarkdown } from "../../lib/markdown";
import { diffStat, type JobEvent, toolCallSummary } from "../../lib/projects";

const props = defineProps<{ events: JobEvent[] }>();

type Row =
	| { kind: "message"; key: string; text: string }
	| { kind: "note"; key: string; text: string }
	| {
			kind: "tool";
			key: string;
			callID: string;
			icon: Component;
			text: string;
			status: "running" | "ok" | "error";
			badge?: string;
			error?: string;
	  };

// The card shows what the agent says and does. Reasoning and token usage
// stay in the run view's timeline.
const rows = computed(() =>
	props.events.flatMap((e, i): Row[] => {
		const key = String(i);
		switch (e.kind) {
			case "message":
				return e.text.trim() ? [{ kind: "message", key, text: e.text }] : [];
			case "tool_call":
				return [
					{
						kind: "tool",
						// normalizeEvents folds each result into its call, so a
						// call keyed by its ID updates in place.
						key: e.callID ? `call:${e.callID}` : key,
						callID: e.callID,
						icon: WrenchScrewdriverIcon,
						text: toolCallSummary(e.name, e.input),
						status: !e.result ? "running" : e.result.error ? "error" : "ok",
						error: e.result?.error ? e.result.output : undefined,
					},
				];
			case "command":
				return [
					{
						kind: "tool",
						key,
						callID: "",
						icon: CommandLineIcon,
						text: e.command,
						status: e.running
							? "running"
							: e.exitCode !== null && e.exitCode !== 0
								? "error"
								: "ok",
						badge: !e.running && e.exitCode ? `exit ${e.exitCode}` : undefined,
					},
				];
			case "file_change": {
				const stat = diffStat(e.diff);
				return [
					{
						kind: "tool",
						key,
						callID: "",
						icon: PencilSquareIcon,
						text: `edit ${e.path}`,
						status: "ok",
						badge: `+${stat.added} −${stat.removed}`,
					},
				];
			}
			case "comment_relay":
				return [
					{
						kind: "tool",
						key,
						callID: "",
						icon: ChatBubbleLeftEllipsisIcon,
						text: `${e.author}: ${e.body}`,
						status: "ok",
						badge: "relayed",
					},
				];
			case "plan": {
				const done = e.items.filter((p) => p.status === "completed").length;
				const current = e.items.find((p) => p.status === "in_progress");
				return [
					{
						kind: "note",
						key,
						text: `Plan ${done}/${e.items.length}${current ? ` · ${current.text}` : ""}`,
					},
				];
			}
			case "system":
				return [{ kind: "note", key, text: e.text }];
			default:
				return [];
		}
	}),
);
</script>
