<template>
	<!-- The prompt half of a task or workflow form: its prompt or script, the
		live analysis under it, the precompile choice, and the model override.
		Only the trigger half differs between editors. -->
	<div class="space-y-3" :data-testid="`${idPrefix}-prompt-section`">
		<div>
			<label :for="`${idPrefix}-prompt`" class="field-label">{{ type === "script" ? "Script" : "Prompt" }}</label>
			<textarea :id="`${idPrefix}-prompt`" v-model="prompt" name="prompt" :data-testid="promptTestId" :rows="rows"
				class="field-input font-mono text-xs" :class="promptClass"
				:placeholder="type === 'script' ? 'print(\'hello from lua\')' : promptPlaceholder" />
			<p v-if="$slots.hint" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
				<slot name="hint" />
			</p>
			<TaskCostFeedback :agent="agent" :prompt="prompt" :type="type" :target="target" :project="project"
				:model="model" :trigger="trigger" :trigger-type="triggerType" :precompile="precompiles" />
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
import { computed, onMounted } from "vue";
import { useAvailableModels } from "../composables/useAvailableModels";
import type { TaskTriggerType } from "../lib/taskAnalysis";
import type { TriggerFields } from "../lib/trigger";
import ModelSelector from "./ModelSelector.vue";
import TaskCostFeedback from "./TaskCostFeedback.vue";

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
	},
);

const { availableModelOptions, refreshCredentials } = useAvailableModels();
onMounted(() => {
	if (props.showModel) void refreshCredentials();
});

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
