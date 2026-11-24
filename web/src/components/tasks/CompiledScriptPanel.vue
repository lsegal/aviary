<template>
	<!-- A precompiling task's compiled script beside its prompt, like a markdown
		preview: read-only, highlighted and numbered, with the compiler's notes,
		its compile attempts, and the actions that change it. -->
	<section data-testid="compiled-panel" :data-stale="stale" aria-label="Compiled script"
		class="flex min-w-0 flex-col overflow-hidden rounded-lg border border-gray-200 bg-gray-50 text-xs dark:border-gray-700 dark:bg-gray-900">
		<header class="flex flex-wrap items-center gap-2 border-b border-gray-200 px-3 py-2 dark:border-gray-700">
			<h4 class="text-xs font-semibold text-gray-700 dark:text-gray-200">Compiled script</h4>
			<CompiledStatusChip :task="task" :attempts="attempts" :edited-prompt="editedPrompt" detail />
			<button v-if="script" type="button" data-testid="compiled-panel-copy"
				class="ml-auto inline-flex items-center gap-1 rounded border border-gray-200 px-2 py-0.5 text-gray-600 hover:bg-white dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
				@click="copy">
				<ClipboardDocumentIcon aria-hidden="true" class="h-3.5 w-3.5" /> {{ copied ? "Copied" : "Copy" }}
			</button>
		</header>

		<div v-if="stale" role="status" data-testid="compiled-panel-stale"
			class="border-b border-amber-200 bg-amber-50 px-3 py-2 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">
			<p>
				<span class="font-semibold">Stale:</span> this script was compiled from prompt version
				<span class="font-mono" data-testid="compiled-panel-source-version">{{ version ? `v${version}` : "unknown" }}</span><template
					v-if="source"> ({{ new Date(source.created_at).toLocaleString() }})</template>, and the prompt has changed since.
				Saving compiles the edited prompt.
			</p>
			<details v-if="source?.prompt" class="mt-1">
				<summary class="cursor-pointer">Prompt it was compiled from</summary>
				<pre data-testid="compiled-panel-source-prompt" class="mt-1 max-h-32 overflow-auto whitespace-pre-wrap font-mono">{{ source.prompt }}</pre>
			</details>
			<p v-if="prediction" data-testid="compiled-panel-prediction" class="mt-1">{{ prediction }}</p>
		</div>

		<div v-if="script" class="max-h-96 min-h-0 overflow-auto bg-white dark:bg-gray-950">
			<ol data-testid="compiled-panel-script" class="min-w-max py-1 font-mono text-[12px] leading-5">
				<li v-for="(line, i) in lines" :key="i" data-testid="compiled-panel-line" :data-line="i + 1"
					class="flex" :class="i + 1 === failedLine ? 'bg-red-50 dark:bg-red-950/40' : ''">
					<span aria-hidden="true" class="w-10 shrink-0 select-none pr-3 text-right text-gray-400">{{ i + 1 }}</span>
					<span class="whitespace-pre pr-3"><span v-for="(tok, j) in line" :key="j" :class="TOKEN_CLS[tok.kind]">{{ tok.text }}</span></span>
				</li>
			</ol>
		</div>
		<p v-else data-testid="compiled-panel-empty" class="px-3 py-4 text-gray-500 dark:text-gray-400">{{ emptyText }}</p>

		<div v-if="notes.length" data-testid="compiled-panel-notes" class="space-y-1 border-t border-gray-200 px-3 py-2 text-gray-600 dark:border-gray-700 dark:text-gray-300">
			<p class="text-[10px] font-semibold uppercase tracking-wide text-gray-400">Compiler notes</p>
			<p v-for="note in notes" :key="note">{{ note }}</p>
		</div>

		<div v-if="attempts.length" data-testid="compiled-panel-attempts" class="border-t border-gray-200 px-3 py-2 dark:border-gray-700">
			<p class="text-[10px] font-semibold uppercase tracking-wide text-gray-400">Compile attempts</p>
			<ul class="mt-1 space-y-0.5">
				<li v-for="a in attempts.slice(0, 5)" :key="a.id" data-testid="compiled-panel-attempt" :data-status="a.status"
					class="flex gap-2 text-gray-600 dark:text-gray-300" :title="a.reason || ''">
					<span class="w-16 shrink-0 font-medium capitalize">{{ a.status }}</span>
					<span class="shrink-0 text-gray-400">{{ age(a.created_at) }} ago</span>
					<span class="truncate">{{ a.reason }}</span>
				</li>
			</ul>
		</div>

		<p v-if="error" role="alert" data-testid="compiled-panel-error" class="border-t border-red-200 bg-red-50 px-3 py-2 text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">{{ error }}</p>

		<div class="flex flex-wrap items-center gap-2 border-t border-gray-200 px-3 py-2 dark:border-gray-700" role="group" aria-label="Compiled script actions">
			<button type="button" data-testid="compiled-panel-recompile" :disabled="busy" :class="ACTION_CLS"
				:title="editedPrompt !== undefined && editedPrompt.trim() !== (task.prompt ?? '').trim() ? 'Compiles the saved prompt; save to compile your edits' : 'Discard the compiled script and compile a fresh one'"
				@click="$emit('recompile')">
				<ArrowPathIcon aria-hidden="true" class="h-3.5 w-3.5" :class="recompiling ? 'animate-spin' : ''" /> Recompile now
			</button>
			<button v-if="task.precompile === true" type="button" data-testid="compiled-panel-disable" :disabled="busy" :class="ACTION_CLS"
				@click="$emit('disable')">
				<NoSymbolIcon aria-hidden="true" class="h-3.5 w-3.5" /> Disable precompile
			</button>
			<router-link v-if="lastFailedRun" data-testid="compiled-panel-last-failed" :to="taskRunPath(agent, task.name, lastFailedRun.id)"
				:title="lastFailedRun.script_error" class="text-blue-600 hover:underline dark:text-blue-400">
				Last failed run ({{ age(lastFailedRun.created_at) }} ago)
			</router-link>
		</div>
	</section>
</template>

<script setup lang="ts">
import ArrowPathIcon from "@heroicons/vue/24/outline/ArrowPathIcon";
import ClipboardDocumentIcon from "@heroicons/vue/24/outline/ClipboardDocumentIcon";
import NoSymbolIcon from "@heroicons/vue/24/outline/NoSymbolIcon";
import { computed, ref } from "vue";
import {
	compiledStatus,
	compiledVersion,
	errorLine,
	highlightLua,
	isStale,
	type LuaTokenKind,
	sourceAttempt,
	type TaskCompileAttempt,
} from "../../lib/compiledStatus";
import { age } from "../../lib/projects";
import type { TaskAnalysis } from "../../lib/taskAnalysis";
import type { Job } from "../../stores/jobs";
import { type AgentTask, taskRunPath } from "../../stores/settings";
import CompiledStatusChip from "./CompiledStatusChip.vue";

const props = defineProps<{
	/** The agent the task belongs to. */
	agent: string;
	/** The task as saved. */
	task: AgentTask;
	/** The prompt as edited. */
	editedPrompt?: string;
	attempts: TaskCompileAttempt[];
	lastFailedRun: Job | null;
	/** The live analysis of the edited prompt, when it has one. */
	analysis?: TaskAnalysis | null;
	busy?: boolean;
	recompiling?: boolean;
	error?: string;
}>();
defineEmits<{ recompile: []; disable: [] }>();

const ACTION_CLS =
	"inline-flex items-center gap-1 rounded border border-gray-200 bg-white px-2 py-0.5 text-gray-700 hover:bg-gray-50 disabled:opacity-50 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200 dark:hover:bg-gray-800";

const TOKEN_CLS: Record<LuaTokenKind, string> = {
	keyword: "text-purple-700 dark:text-purple-300",
	string: "text-green-700 dark:text-green-300",
	comment: "italic text-gray-400",
	number: "text-amber-700 dark:text-amber-300",
	builtin: "text-blue-700 dark:text-blue-300",
	text: "text-gray-800 dark:text-gray-200",
};

const script = computed(() => props.task.compiled?.trim() ?? "");
const lines = computed(() => highlightLua(script.value));
const version = computed(() => compiledVersion(props.task));
const source = computed(() => sourceAttempt(props.task, props.attempts));
const stale = computed(() =>
	isStale(props.task, {
		editedPrompt: props.editedPrompt,
		attempts: props.attempts,
	}),
);
const status = computed(() =>
	compiledStatus(props.task, {
		editedPrompt: props.editedPrompt,
		attempts: props.attempts,
	}),
);

/** The line the last failure's Lua error points at, highlighted in the script. */
const failedLine = computed(() =>
	errorLine(props.lastFailedRun?.script_error || props.task.precompile_note),
);

/** What live analysis of the edited prompt predicts a recompile does. */
const prediction = computed(() => {
	const a = props.analysis;
	if (!a) return "";
	if (!a.will_precompile)
		return `Live analysis predicts the edited prompt would not compile (${a.verdict}${a.reason ? `: ${a.reason}` : ""}), so runs would use the model.`;
	return "Live analysis predicts the edited prompt compiles to a different script when saved.";
});

const notes = computed(() => {
	const out: string[] = [];
	const add = (n?: string) => {
		const t = n?.trim();
		if (t && !out.includes(t)) out.push(t);
	};
	if (script.value) add(source.value?.reason);
	else add(props.attempts[0]?.reason);
	add(props.task.precompile_note);
	return out;
});

const emptyText = computed(() => {
	switch (status.value?.state) {
		case "compiling":
			return "Compiling… the script shows here once it is compiled.";
		case "failed":
			return "The last compile attempt failed, so there is no compiled script.";
		case "not-compiled":
			return "The prompt is not deterministic enough to compile, so runs use the model.";
		case "fallback":
			return "The compiled script was set aside after it failed, so runs use the model.";
		default:
			return "Not compiled yet. Save with precompile on to compile the prompt.";
	}
});

const copied = ref(false);
async function copy() {
	try {
		await navigator.clipboard.writeText(script.value);
		copied.value = true;
		setTimeout(() => {
			copied.value = false;
		}, 1500);
	} catch {
		// Clipboard access can be denied; the script stays selectable.
	}
}
</script>
