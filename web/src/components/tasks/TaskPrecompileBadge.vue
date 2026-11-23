<template>
	<span v-if="badge" data-testid="task-precompile-badge" :data-state="badge.state" :title="badge.title"
		class="inline-block max-w-full shrink-0 truncate whitespace-nowrap rounded-full px-2 py-0.5 align-middle text-[11px] font-semibold" :class="TONES[badge.tone]">
		{{ badge.label }}
	</span>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { type AgentTask, precompileState } from "../../stores/settings";

// A precompiling task's compiled state as a badge: compiled, compiling, not
// deterministic, or running on the model after its script failed.

const props = defineProps<{
	task: AgentTask;
	/** Leaves the failure count to the title, for narrow cards. */
	compact?: boolean;
}>();

const TONES = {
	green: "bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300",
	amber: "bg-amber-50 text-amber-800 dark:bg-amber-950/50 dark:text-amber-300",
	gray: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300",
} as const;

const badge = computed(() => {
	const t = props.task;
	const failures = t.script_failures ?? 0;
	switch (precompileState(t)) {
		case "compiled":
			return failures
				? {
						state: "compiled",
						tone: "amber" as const,
						label: props.compact ? "Compiled" : `Compiled · ${failures} failed`,
						title: `The compiled script failed its last ${failures === 1 ? "run" : `${failures} runs`}, which fell back to the model.`,
					}
				: {
						state: "compiled",
						tone: "green" as const,
						label: "Compiled",
						title:
							"Runs execute the compiled script with no model calls, and fall back to the model if it fails.",
					};
		case "set-aside":
			return {
				state: "set-aside",
				tone: "amber" as const,
				label: "Runs on model",
				title: t.precompile_note ?? "",
			};
		case "not-deterministic":
			return {
				state: "not-deterministic",
				tone: "gray" as const,
				label: "Not precompiled",
				title:
					"The prompt is not deterministic enough to compile, so runs use the model.",
			};
		case "compiling":
			return {
				state: "compiling",
				tone: "gray" as const,
				label: "Compiling…",
				title:
					"Its script is compiled in the background; runs until then compile first.",
			};
		default:
			return null;
	}
});
</script>
