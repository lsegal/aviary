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
				<button v-if="state === 'error'" type="button" data-testid="task-cost-retry"
					class="ml-2 text-blue-600 hover:underline dark:text-blue-400" @click="retry">Retry</button>
			</p>
			<p v-if="note" data-testid="task-cost-note" class="text-gray-500 dark:text-gray-400">{{ note }}</p>
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
import { formatEstimate, parseTaskAnalysis } from "../lib/taskAnalysis";

/**
 * Live feedback under a task's prompt: whether the precompile step would
 * compile it into a script, and what each run is estimated to cost.
 */
const props = withDefaults(
	defineProps<{
		agent: string;
		prompt: string;
		/** The task type: prompt, script, or project. */
		type?: string;
		target?: string;
		model?: string;
		/** Whether saving the editor precompiles the task. */
		precompile?: boolean;
		/** How long typing must pause before the prompt is analyzed. */
		debounceMs?: number;
	}>(),
	{
		type: "prompt",
		target: "",
		model: "",
		precompile: false,
		debounceMs: 1200,
	},
);

const { callTool } = useMCP();
const editor = `task-editor-${Math.random().toString(36).slice(2)}`;

type State = "idle" | "checking" | "ready" | "error" | "script" | "project";
const state = ref<State>("idle");
const result = ref<TaskAnalysis | null>(null);
let timer: ReturnType<typeof setTimeout> | undefined;
// Only the latest request may set the result; older ones are discarded.
let seq = 0;

const input = computed(() => ({
	agent: props.agent,
	content: props.prompt.trim(),
	type: props.type || "prompt",
	target: props.target,
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
	} catch {
		if (id !== seq) return;
		result.value = null;
		state.value = "error";
	}
}

watch(
	inputKey,
	() => {
		clearTimer();
		seq++;
		const { agent, content, type } = input.value;
		if (type === "script") {
			state.value = "script";
			return;
		}
		if (type === "project") {
			state.value = "project";
			return;
		}
		if (!agent || !content) {
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

function retry() {
	void analyze();
}

const icon = computed(() => {
	if (state.value === "checking")
		return { glyph: "…", class: "text-gray-400 animate-pulse" };
	if (state.value === "error") return { glyph: "!", class: "text-amber-500" };
	if (state.value === "script") return { glyph: "✔", class: "text-green-600" };
	if (state.value === "project") return { glyph: "ℹ", class: "text-gray-400" };
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
			return "Couldn't analyze";
		case "script":
			return "Runs a script";
		case "project":
			return "Project tasks are never precomputed";
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
	if (state.value === "project")
		return "Estimate unavailable: depends on the work the Projects job does";
	if (state.value !== "ready" || !result.value) return "";
	return formatEstimate(result.value.estimate);
});

const note = computed(() => {
	const r = result.value;
	if (state.value !== "ready" || !r) return "";
	switch (r.verdict) {
		case "deterministic":
			return r.will_precompile
				? "It will be precompiled into a script, so runs need no model calls."
				: "Precompiling it into a script would let runs skip the model; until then each run uses the model.";
		case "partial": {
			const steps = r.model_steps?.join("; ");
			return `${steps ? `${capitalize(steps)} needs the model, so` : "Some steps need the model, so"} the task is not precompiled and runs through the model.`;
		}
		default:
			return r.reason ? `Won't be precompiled: ${r.reason}` : "";
	}
});

function capitalize(s: string): string {
	return s.charAt(0).toUpperCase() + s.slice(1);
}
</script>
