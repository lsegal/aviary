<template>
	<section :aria-label="title" data-testid="task-run-log" :data-run="entry.id"
		class="flex min-h-0 flex-col overflow-hidden rounded-xl border border-gray-200 dark:border-gray-800">
		<header class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-gray-200 px-3 py-2 text-xs dark:border-gray-800">
			<h3 class="text-sm font-semibold text-gray-900 dark:text-white" data-testid="task-run-log-title">{{ title }}</h3>
			<TaskRunResultPill :result="entry.result" />
			<span class="text-gray-500 dark:text-gray-400" data-testid="task-run-log-started">
				started {{ startedText }}
			</span>
			<span class="tabular-nums text-gray-500 dark:text-gray-400" data-testid="task-run-log-elapsed">
				{{ entry.result === "running" ? "elapsed" : "took" }} {{ runDurationText(entry, now) }}
			</span>
			<span v-if="entry.trigger" class="text-gray-500 dark:text-gray-400">via {{ triggerLabel(entry.trigger) }}</span>
			<div class="ml-auto flex items-center gap-1">
				<slot name="actions" />
				<button type="button" data-testid="task-run-log-copy" :aria-label="copied ? 'Copied' : 'Copy logs'"
					:title="copied ? 'Copied' : 'Copy logs'" :disabled="!logs"
					class="rounded-md p-1 text-gray-500 hover:bg-gray-100 hover:text-gray-900 disabled:opacity-40 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-white"
					@click="copy">
					<component :is="copied ? CheckIcon : ClipboardDocumentIcon" aria-hidden="true" class="h-4 w-4" />
				</button>
				<button v-if="isActive(entry) && entry.job" type="button" data-testid="task-run-log-stop" aria-label="Stop run"
					title="Stop run" :disabled="stopping"
					class="rounded-md p-1 text-red-600 hover:bg-red-50 disabled:opacity-40 dark:text-red-400 dark:hover:bg-red-950/40"
					@click="onStop">
					<StopIcon aria-hidden="true" class="h-4 w-4" />
				</button>
			</div>
		</header>
		<div v-if="entry.error" role="alert" data-testid="task-run-log-error"
			class="space-y-1 border-b border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
			<div class="flex items-start gap-2">
				<p data-testid="task-run-log-error-message" class="line-clamp-3 min-w-0 flex-1 whitespace-pre-wrap wrap-anywhere"
					:title="entry.error">{{ entry.error }}</p>
				<a v-if="entry.bugReportUrl" :href="entry.bugReportUrl" target="_blank" rel="noopener noreferrer"
					data-testid="task-run-log-report-bug"
					class="inline-flex shrink-0 items-center gap-1 rounded-md border border-red-300 px-2 py-0.5 font-medium hover:bg-red-100 dark:border-red-800 dark:hover:bg-red-900/40">
					<BugAntIcon aria-hidden="true" class="h-3.5 w-3.5" /> Report bug
				</a>
			</div>
			<details v-if="entry.errorOutput" data-testid="task-run-log-error-output">
				<summary class="cursor-pointer select-none">Last agent output</summary>
				<pre class="mt-1 max-h-40 overflow-y-auto rounded bg-red-100/60 px-2 py-1 font-mono whitespace-pre-wrap wrap-anywhere dark:bg-red-950/60">{{ entry.errorOutput }}</pre>
			</details>
		</div>
		<p v-if="delivery" data-testid="task-run-log-delivery" :data-status="delivery.status"
			:role="delivery.status === 'failed' ? 'alert' : undefined" class="border-b px-3 py-2 text-xs"
			:class="delivery.status === 'failed' ? 'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300' : 'border-gray-200 text-gray-600 dark:border-gray-800 dark:text-gray-300'">
			<span class="font-medium">{{ delivery.status === "skipped" ? "Skipped reply" : `Delivery ${delivery.status}` }}:</span> {{ delivery.message }}
			<router-link v-if="deliveryJobPath" :to="deliveryJobPath" data-testid="task-run-log-delivery-link"
				class="ml-1 text-blue-600 hover:underline dark:text-blue-400">Open job</router-link>
		</p>
		<div class="relative flex min-h-0 flex-1 flex-col">
			<pre ref="logEl" data-testid="task-run-log-output"
				class="min-h-24 flex-1 overflow-y-auto whitespace-pre-wrap break-all bg-slate-950 px-3 py-2 font-mono text-xs leading-relaxed text-slate-200"
				@scroll="onScroll">{{ logs || placeholder }}</pre>
			<MoreBelowPill v-if="showMore" :count="newCount" data-testid="task-run-log-follow" @click="scrollToBottom()" />
		</div>
	</section>
</template>

<script setup lang="ts">
import BugAntIcon from "@heroicons/vue/24/outline/BugAntIcon";
import CheckIcon from "@heroicons/vue/24/outline/CheckIcon";
import ClipboardDocumentIcon from "@heroicons/vue/24/outline/ClipboardDocumentIcon";
import StopIcon from "@heroicons/vue/24/outline/StopIcon";
import { computed, onUnmounted, ref, watch } from "vue";
import { useAutoFollow } from "../../../composables/useAutoFollow";
import { jobPath } from "../../../lib/projects";
import {
	isActive,
	runDurationText,
	type TaskRunEntry,
	triggerLabel,
} from "../../../lib/taskRuns";
import MoreBelowPill from "../../MoreBelowPill.vue";
import TaskRunResultPill from "./TaskRunResultPill.vue";

// A run's logs: streamed while it runs, kept scrolled to the newest line
// unless the user scrolls up, with copy and stop.

const LIVE_POLL_MS = 2_000;

const props = defineProps<{
	entry: TaskRunEntry;
	title: string;
	now: number;
	fetchLogs: (jobID: string) => Promise<string>;
}>();
const emit = defineEmits<{ stop: [jobID: string] }>();

const delivery = computed(() => props.entry.job?.delivery);
const deliveryJobPath = computed(() => {
	const d = delivery.value;
	return d?.project && d.number
		? jobPath(d.owner ?? "", d.project, d.number)
		: "";
});

const logs = ref("");
const loading = ref(false);
const placeholder = computed(() => {
	if (!props.entry.job)
		return "This fire never started a run, so it has no logs.";
	if (loading.value) return "Loading logs…";
	return props.entry.result === "queued"
		? "Waiting for the run to start…"
		: "No output yet.";
});

const startedText = computed(() =>
	new Date(props.entry.startedAt).toLocaleString(),
);

let timer: ReturnType<typeof setTimeout> | undefined;
async function load() {
	clearTimeout(timer);
	const job = props.entry.job;
	if (!job) return;
	const id = job.id;
	try {
		const text = await props.fetchLogs(id);
		if (props.entry.job?.id === id) logs.value = text;
	} catch (e) {
		if (props.entry.job?.id === id)
			logs.value = `Error: ${e instanceof Error ? e.message : String(e)}`;
	} finally {
		loading.value = false;
	}
	if (props.entry.job?.id === id && isActive(props.entry))
		timer = setTimeout(() => void load(), LIVE_POLL_MS);
}

watch(
	() => props.entry.id,
	() => {
		logs.value = "";
		loading.value = true;
		void load();
	},
	{ immediate: true },
);
// One last fetch when the run finishes picks up its final output.
watch(
	() => props.entry.result,
	(result, before) => {
		if (result !== before) void load();
	},
);
onUnmounted(() => clearTimeout(timer));

const logEl = ref<HTMLElement | null>(null);
const { showMore, newCount, onScroll, scrollToBottom } = useAutoFollow(logEl, {
	count: () => (logs.value ? logs.value.split("\n").length : 0),
});
// Another run's log opens at its newest line.
watch(
	() => props.entry.id,
	() => scrollToBottom({ smooth: false }),
);

const copied = ref(false);
async function copy() {
	await navigator.clipboard.writeText(logs.value);
	copied.value = true;
	setTimeout(() => {
		copied.value = false;
	}, 1500);
}

const stopping = ref(false);
function onStop() {
	const job = props.entry.job;
	if (!job) return;
	stopping.value = true;
	emit("stop", job.id);
}
watch(
	() => props.entry.result,
	() => {
		stopping.value = false;
	},
);
</script>
