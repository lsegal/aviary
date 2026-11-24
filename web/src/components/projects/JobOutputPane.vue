<template>
	<div class="relative flex min-h-0 flex-[1_1_7rem] flex-col">
		<div v-if="rich" ref="outputEl" :data-testid="`${testid}-rich`"
			class="min-h-0 flex-1 overflow-y-auto bg-slate-950 px-4 py-3 text-xs text-slate-200"
			@scroll="onScroll">
			<JobOutputRows :events="job.events" />
		</div>
		<pre v-else ref="outputEl" :data-testid="testid"
			class="min-h-0 flex-1 overflow-y-auto whitespace-pre-wrap break-all bg-slate-950 px-4 py-3 font-mono text-xs leading-relaxed text-slate-200"
			@scroll="onScroll">{{ outputText }}</pre>
		<MoreBelowPill v-if="showMore" :count="newCount" :data-testid="`${testid}-more-below`" @click="scrollToBottom()" />
	</div>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { useAutoFollow } from "../../composables/useAutoFollow";
import { useJobOutputMode } from "../../composables/useJobOutput";
import type { ProjectJob } from "../../lib/projects";
import MoreBelowPill from "../MoreBelowPill.vue";
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
const { showMore, newCount, onScroll, scrollToBottom } = useAutoFollow(
	outputEl,
	{
		count: () =>
			rich.value ? props.job.events.length : props.job.output.length,
	},
);
</script>
