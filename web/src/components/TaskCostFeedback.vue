<template>
	<div v-if="state !== 'idle'" data-testid="task-cost-feedback" :data-state="state" role="status" aria-live="polite"
		class="mt-1.5 flex items-start gap-2 text-xs">
		<span aria-hidden="true" class="w-4 shrink-0 text-center" :class="icon.class">{{ icon.glyph }}</span>
		<div class="min-w-0 flex-1">
			<p class="text-gray-700 dark:text-gray-200">
				<span data-testid="task-cost-verdict" class="font-medium">{{ headline }}</span>
				<template v-if="estimate">
					<span class="text-gray-400"> · </span>
					<span data-testid="task-cost-estimate">{{ estimate }}</span>
				</template>
				<template v-if="monthly">
					<span class="text-gray-400"> · </span>
					<span data-testid="task-cost-monthly">{{ monthly }}</span>
				</template>
			</p>
			<p v-if="note" data-testid="task-cost-note" class="text-gray-500 dark:text-gray-400">{{ note }}</p>
			<template v-if="state === 'error'">
				<p data-testid="task-cost-error" class="break-words text-gray-500 dark:text-gray-400">{{ errorMessage }}</p>
				<button type="button" data-testid="task-cost-retry"
					class="mt-1 rounded border border-gray-300 px-2 py-0.5 text-blue-600 hover:bg-gray-50 dark:border-gray-600 dark:text-blue-400 dark:hover:bg-gray-800"
					@click="retry">Retry analysis</button>
			</template>
		</div>
	</div>
</template>

<script lang="ts">
import type { TaskAnalysis } from "../lib/taskAnalysis";

/** Analyses by input, shared by every editor, so unchanged input is not re-analyzed. */
const cache = new Map<string, TaskAnalysis>();
</script>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useMCP } from "../composables/useMCP";
import { useTriggerRuns } from "../composables/useTriggerPreview";
import {
	formatEstimate,
	formatMonthlyEstimate,
	parseTaskAnalysis,
} from "../lib/taskAnalysis";
import type { TriggerFields } from "../lib/trigger";

/**
 * Live feedback under a task's prompt: whether the precompile step would
 * compile it into a script, and what each run is estimated to cost, plus
 * what a month of runs costs when the task has a recurring trigger.
 */
const props = withDefaults(
	defineProps<{
		agent: string;
		prompt: string;
		/** The task type: prompt, script, or project. */
		type?: string;
		target?: string;
		/** A project task's watched project, which its script runs in a clone of. */
		project?: string;
		model?: string;
		/** The task's trigger as edited, for the monthly projection. */
		trigger?: TriggerFields | null;
		/** Whether saving the editor precompiles the task. */
		precompile?: boolean;
		/** How long typing must pause before the prompt is analyzed. */
		debounceMs?: number;
	}>(),
	{
		type: "prompt",
		target: "",
		project: "",
		model: "",
		trigger: null,
		precompile: false,
		debounceMs: 1200,
	},
);

const { callTool } = useMCP();
const editor = `task-editor-${Math.random().toString(36).slice(2)}`;

type State = "idle" | "checking" | "ready" | "error" | "script";
const state = ref<State>("idle");
const result = ref<TaskAnalysis | null>(null);
const errorMessage = ref("");
let timer: ReturnType<typeof setTimeout> | undefined;
// Only the latest request may set the result; older ones are discarded.
let seq = 0;

const input = computed(() => ({
	agent: props.agent,
	content: props.prompt.trim(),
	type: props.type || "prompt",
	target: props.target,
	project: props.type === "project" ? props.project : "",
	model: props.model,
	precompile: props.precompile,
}));
const inputKey = computed(() => JSON.stringify(input.value));

function clearTimer() {
	if (timer) clearTimeout(timer);
	timer = undefined;
}

async function analyze() {
	clearTimer();
	const args = input.value;
	const key = inputKey.value;
	const id = ++seq;
	state.value = "checking";
	try {
		const text = await callTool("task_analyze", {
			agent: args.agent,
			content: args.content,
			type: args.type,
			...(args.target ? { target: args.target } : {}),
			...(args.project ? { project: args.project } : {}),
			...(args.model ? { model: args.model } : {}),
			precompile: args.precompile,
			editor,
		});
		const parsed = parseTaskAnalysis(text);
		if (!parsed) throw new Error("unexpected task_analyze result");
		cache.set(key, parsed);
		if (id !== seq) return;
		result.value = parsed;
		state.value = "ready";
	} catch (e) {
		if (id !== seq) return;
		const message = e instanceof Error ? e.message : String(e);
		// The server cancels an analysis this editor replaced; the newer
		// request's result is the one to show.
		if (message.includes("superseded by a newer request")) return;
		console.warn("task_analyze failed:", message);
		result.value = null;
		errorMessage.value = message || "The analysis failed.";
		state.value = "error";
	}
}

watch(
	inputKey,
	() => {
		clearTimer();
		seq++;
		const { agent, content, type, project } = input.value;
		if (type === "script") {
			state.value = "script";
			return;
		}
		if (!agent || !content || (type === "project" && !project)) {
			state.value = "idle";
			return;
		}
		const hit = cache.get(inputKey.value);
		if (hit) {
			result.value = hit;
			state.value = "ready";
			return;
		}
		// The old verdict no longer describes the input; the request itself
		// waits for typing to pause.
		state.value = "checking";
		timer = setTimeout(() => void analyze(), props.debounceMs);
	},
	{ immediate: true },
);

onBeforeUnmount(() => {
	clearTimer();
	seq++;
});

// How often the trigger fires in 30 days, from task_preview so the count
// follows the scheduler's own trigger semantics.
const triggerRuns = useTriggerRuns();
const runs = ref(0);
let runsTimer: ReturnType<typeof setTimeout> | undefined;
let runsSeq = 0;
watch(
	() => JSON.stringify(props.trigger ?? null),
	() => {
		if (runsTimer) clearTimeout(runsTimer);
		const id = ++runsSeq;
		const trigger = props.trigger;
		if (!trigger?.trigger || trigger.trigger === "once") {
			runs.value = 0;
			return;
		}
		runsTimer = setTimeout(async () => {
			const n = await triggerRuns(trigger);
			if (id === runsSeq) runs.value = n;
		}, 300);
	},
	{ immediate: true },
);
onBeforeUnmount(() => {
	if (runsTimer) clearTimeout(runsTimer);
	runsSeq++;
});

function retry() {
	void analyze();
}

const icon = computed(() => {
	if (state.value === "checking")
		return { glyph: "…", class: "text-gray-400 animate-pulse" };
	if (state.value === "error") return { glyph: "!", class: "text-amber-500" };
	if (state.value === "script") return { glyph: "✔", class: "text-green-600" };
	switch (result.value?.verdict) {
		case "deterministic":
			return { glyph: "✔", class: "text-green-600" };
		case "partial":
			return { glyph: "◐", class: "text-amber-500" };
		default:
			return { glyph: "✖", class: "text-red-500" };
	}
});

const headline = computed(() => {
	switch (state.value) {
		case "checking":
			return "Checking…";
		case "error":
			return "Couldn't analyze this task.";
		case "script":
			return "Runs a script";
	}
	switch (result.value?.verdict) {
		case "deterministic":
			return "This task can run deterministically";
		case "partial":
			return "Partially deterministic";
		default:
			return "Not deterministic";
	}
});

const estimate = computed(() => {
	if (state.value === "script") return "0 tokens/run";
	if (state.value !== "ready" || !result.value) return "";
	return formatEstimate(result.value.estimate);
});

const monthly = computed(() => {
	if (state.value !== "ready" || !result.value) return "";
	return formatMonthlyEstimate(result.value.estimate, runs.value);
});

const note = computed(() => {
	const r = result.value;
	if (state.value !== "ready" || !r) return "";
	switch (r.verdict) {
		case "deterministic":
			if (props.type === "project")
				return r.will_precompile
					? "It will be precompiled into a script that runs in a clone of the project with no model calls; a run whose script fails falls back to the model."
					: "Precompiling it would let runs skip the model in the project's clone; until then each run uses the model.";
			return r.will_precompile
				? "It will be precompiled into a script, so runs need no model calls."
				: "Precompiling it into a script would let runs skip the model; until then each run uses the model.";
		case "partial": {
			const steps = r.model_steps?.join("; ");
			return `${steps ? `${capitalize(steps)} needs the model, so` : "Some steps need the model, so"} the task is not precompiled and runs through the model.`;
		}
		default:
			return r.reason ? `Can't precompile: ${r.reason}` : "";
	}
});

function capitalize(s: string): string {
	return s.charAt(0).toUpperCase() + s.slice(1);
}
</script>
