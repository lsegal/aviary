<template>
	<div v-if="rich" ref="outputEl" :data-testid="`${testid}-rich`"
		class="min-h-0 flex-[1_1_7rem] overflow-y-auto border-t border-gray-100 px-4 py-3 dark:border-gray-800"
		@scroll="onScroll">
		<JobOutputRows :events="job.events" />
	</div>
	<pre v-else ref="outputEl" :data-testid="testid"
		class="min-h-0 flex-[1_1_7rem] overflow-y-auto whitespace-pre-wrap break-all bg-slate-950 px-4 py-3 font-mono text-xs leading-relaxed text-slate-200"
		@scroll="onScroll">{{ outputText }}</pre>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import {
	useJobOutputMode,
	useStickToBottom,
} from "../../composables/useJobOutput";
import type { ProjectJob } from "../../lib/projects";
import JobOutputRows from "./JobOutputRows.vue";

// A board card's job output: the rich transcript or the raw scrollback, as
// the shared output toggle selects, kept scrolled to its newest line.

const props = withDefaults(
	defineProps<{
		job: Pick<ProjectJob, "events" | "output">;
		/** The raw pane's test id; the rich pane appends `-rich`. */
		testid?: string;
	}>(),
	{ testid: "job-output" },
);

const outputText = computed(() =>
	props.job.output.length
		? props.job.output.slice(-12).join("\n")
		: "No output yet.",
);

// Rich output falls back to the scrollback for a job with no events.
const mode = useJobOutputMode();
const rich = computed(
	() => mode.value === "rich" && props.job.events.length > 0,
);
const outputEl = ref<HTMLElement | null>(null);
const { onScroll } = useStickToBottom(outputEl, () => [
	props.job.events,
	props.job.output,
]);
</script>
