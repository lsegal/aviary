<template>
	<!-- The prompt half of a task or workflow form: its prompt or script, the
		live analysis under it, the precompile choice, and the model override.
		Only the trigger half differs between editors. -->
	<div class="space-y-3" :data-testid="`${idPrefix}-prompt-section`">
		<div>
			<div class="flex flex-wrap items-center gap-2">
				<label :for="`${idPrefix}-prompt`" class="field-label">{{ type === "script" ? "Script" : "Prompt" }}</label>
				<CompiledStatusChip v-if="showCompiled" :task="savedTask!" :attempts="compiles.attempts.value" :edited-prompt="prompt"
					detail clickable @open="panelOpen = true" />
				<button v-if="showCompiled" type="button" :data-testid="`${idPrefix}-compiled-toggle`" :aria-pressed="panelOpen"
					:aria-controls="`${idPrefix}-compiled-panel`"
					class="ml-auto inline-flex items-center gap-1 rounded border px-2 py-0.5 text-xs"
					:class="panelOpen ? 'border-blue-600 bg-blue-50 text-blue-700 dark:border-blue-500 dark:bg-blue-950/50 dark:text-blue-300' : 'border-gray-200 text-gray-600 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800'"
					@click="panelOpen = !panelOpen">
					<CodeBracketIcon aria-hidden="true" class="h-3.5 w-3.5" /> {{ panelOpen ? "Hide compiled" : "Show compiled" }}
				</button>
			</div>
			<!-- The compiled script sits beside the prompt, like a markdown
				preview, and stacks below it on narrow screens. -->
			<div :class="showCompiled && panelOpen ? 'grid min-w-0 gap-3 md:grid-cols-2' : ''">
				<div class="min-w-0">
					<textarea :id="`${idPrefix}-prompt`" v-model="prompt" name="prompt" :data-testid="promptTestId" :rows="rows"
						class="field-input font-mono text-xs" :class="promptClass"
						:placeholder="type === 'script' ? 'print(\'hello from lua\')' : promptPlaceholder" />
					<p v-if="$slots.hint" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
						<slot name="hint" />
					</p>
					<TaskCostFeedback :agent="agent" :prompt="prompt" :type="type" :target="target" :project="project"
						:model="model" :trigger="trigger" :trigger-type="triggerType" :precompile="precompiles"
						@analysis="analysis = $event" />
				</div>
				<CompiledScriptPanel v-if="showCompiled && panelOpen" :id="`${idPrefix}-compiled-panel`" :agent="taskAgent || agent"
					:task="savedTask!" :edited-prompt="prompt" :attempts="compiles.attempts.value"
					:last-failed-run="compiles.lastFailedRun.value" :analysis="analysis" :busy="compiles.busy.value"
					:recompiling="compiles.recompiling.value" :error="compiles.error.value"
					@recompile="compiles.recompile()" @disable="disable" />
			</div>
		</div>

		<div v-if="showPrecompile" class="flex items-start gap-3" :class="canPrecompile ? '' : 'opacity-60'">
			<input :id="`${idPrefix}-precompile`" v-model="precompile" type="checkbox" name="precompile"
				:disabled="!canPrecompile" :aria-describedby="`${idPrefix}-precompile-help`"
				class="mt-0.5 h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500 disabled:cursor-not-allowed dark:border-gray-600 dark:bg-gray-800" />
			<div>
				<label :for="`${idPrefix}-precompile`" class="text-sm text-gray-700 dark:text-gray-300"
					:class="canPrecompile ? 'cursor-pointer' : 'cursor-not-allowed'">Try to precompile</label>
				<p :id="`${idPrefix}-precompile-help`" :data-testid="`${idPrefix}-precompile-help`"
					class="text-xs text-gray-500 dark:text-gray-400">{{ help }}</p>
			</div>
		</div>

		<div v-if="showModel" :data-testid="`${idPrefix}-model`">
			<span class="field-label">Model override (optional)</span>
			<ModelSelector :model-value="model" :options="availableModelOptions" :placeholder="modelPlaceholder"
				@update:model-value="model = typeof $event === 'string' ? $event : ''" />
			<p v-if="modelHelp" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ modelHelp }}</p>
		</div>
	</div>
</template>

<script setup lang="ts">
import CodeBracketIcon from "@heroicons/vue/24/outline/CodeBracketIcon";
import { computed, onMounted, ref } from "vue";
import { useAvailableModels } from "../composables/useAvailableModels";
import {
	useCompiledPanelOpen,
	useTaskCompiles,
} from "../composables/useTaskCompiles";
import type { TaskAnalysis, TaskTriggerType } from "../lib/taskAnalysis";
import type { TriggerFields } from "../lib/trigger";
import { type AgentTask, precompilesBesidePrompt } from "../stores/settings";
import ModelSelector from "./ModelSelector.vue";
import TaskCostFeedback from "./TaskCostFeedback.vue";
import CompiledScriptPanel from "./tasks/CompiledScriptPanel.vue";
import CompiledStatusChip from "./tasks/CompiledStatusChip.vue";

const prompt = defineModel<string>("prompt", { required: true });
const precompile = defineModel<boolean>("precompile", { default: false });
const model = defineModel<string>("model", { default: "" });

const props = withDefaults(
	defineProps<{
		/** Prefixes the section's element IDs and test IDs. */
		idPrefix: string;
		/** The agent whose model and tools the prompt is analyzed with. */
		agent: string;
		/** The task type: prompt, script, or project. */
		type?: string;
		target?: string;
		project?: string;
		/** The task's schedule as edited, for the monthly projection. */
		trigger?: TriggerFields | null;
		/** What starts the task's runs: a schedule, or a message for a workflow. */
		triggerType?: TaskTriggerType;
		showPrecompile?: boolean;
		/** Replaces the precompile help, such as with the task's compiled state. */
		precompileHelp?: string;
		showModel?: boolean;
		modelPlaceholder?: string;
		modelHelp?: string;
		promptPlaceholder?: string;
		promptTestId?: string;
		promptClass?: string;
		rows?: number;
		/** The task as saved, whose compiled status and script show beside the prompt. */
		savedTask?: AgentTask;
		/** The agent the saved task belongs to, when it differs from agent. */
		taskAgent?: string;
	}>(),
	{
		type: "prompt",
		target: "",
		project: "",
		trigger: null,
		triggerType: "schedule",
		showPrecompile: true,
		precompileHelp: "",
		showModel: false,
		modelPlaceholder: "Default agent model",
		modelHelp: "",
		promptPlaceholder: "Task prompt...",
		promptTestId: undefined,
		promptClass: "",
		rows: 6,
		savedTask: undefined,
		taskAgent: "",
	},
);

const { availableModelOptions, refreshCredentials } = useAvailableModels();
onMounted(() => {
	if (props.showModel) void refreshCredentials();
});

// ── Compiled script ─────────────────────────────────────────────────────────

/** Whether the saved task precompiles beside its prompt, so its compiled state shows. */
const showCompiled = computed(
	() =>
		!!props.savedTask &&
		precompilesBesidePrompt(props.savedTask) &&
		props.type !== "script" &&
		(props.savedTask.precompile === true || !!props.savedTask.precompile_note),
);
const panelOpen = useCompiledPanelOpen();
const analysis = ref<TaskAnalysis | null>(null);
const compiles = useTaskCompiles(
	computed(() => props.taskAgent || props.agent),
	computed(() => (showCompiled.value ? props.savedTask : undefined)),
);
async function disable() {
	await compiles.disablePrecompile();
	if (!compiles.error.value) precompile.value = false;
}

/** A script is already a script, so only prompts precompile. */
const canPrecompile = computed(() => props.type !== "script");
/** Whether saving precompiles the task, which the analysis prices. */
const precompiles = computed(
	() => props.showPrecompile && canPrecompile.value && precompile.value,
);

const help = computed(() => {
	if (props.precompileHelp) return props.precompileHelp;
	if (!canPrecompile.value) return "Script tasks are already scripts";
	if (props.triggerType === "message")
		return "Compile a deterministic prompt into a script that runs on each matching message without the model; a run whose script fails falls back to the model";
	if (props.type === "project")
		return "Compile a deterministic prompt into a script that runs in the project's clone without the model; a run whose script fails falls back to the model";
	return "Compile deterministic prompt tasks into scripts before scheduling recurring or delayed runs";
});
</script>
