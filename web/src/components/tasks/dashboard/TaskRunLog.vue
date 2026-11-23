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
			<span class="text-gray-500 dark:text-gray-400">· {{ triggerLabel(entry.trigger) }}</span>
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
		<p v-if="entry.error" role="alert" data-testid="task-run-log-error"
			class="border-b border-red-200 bg-red-50 px-3 py-2 text-xs whitespace-pre-wrap text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
			{{ entry.error }}
		</p>
		<div class="relative flex min-h-0 flex-1 flex-col">
			<pre ref="logEl" data-testid="task-run-log-output"
				class="min-h-24 flex-1 overflow-y-auto whitespace-pre-wrap break-all bg-slate-950 px-3 py-2 font-mono text-xs leading-relaxed text-slate-200"
				@scroll="onScroll">{{ logs || placeholder }}</pre>
			<button v-if="!following" type="button" data-testid="task-run-log-follow"
				class="absolute right-3 bottom-2 rounded-full bg-blue-600 px-2.5 py-0.5 text-xs font-medium text-white shadow hover:bg-blue-700"
				@click="follow">
				Auto-scroll paused · Jump to latest
			</button>
		</div>
	</section>
</template>

<script setup lang="ts">
import CheckIcon from "@heroicons/vue/24/outline/CheckIcon";
import ClipboardDocumentIcon from "@heroicons/vue/24/outline/ClipboardDocumentIcon";
import StopIcon from "@heroicons/vue/24/outline/StopIcon";
import { computed, onUnmounted, ref, watch } from "vue";
import { useStickToBottom } from "../../../composables/useJobOutput";
import {
	isActive,
	runDurationText,
	type TaskRunEntry,
	triggerLabel,
} from "../../../lib/taskRuns";
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
const { following, onScroll } = useStickToBottom(logEl, () => logs.value);

function follow() {
	following.value = true;
	if (logEl.value) logEl.value.scrollTop = logEl.value.scrollHeight;
}

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
