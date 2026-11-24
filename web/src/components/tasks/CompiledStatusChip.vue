<template>
	<component :is="to ? RouterLink : clickable ? 'button' : 'span'" v-if="status" :to="to" :type="clickable && !to ? 'button' : undefined"
		data-testid="compiled-status-chip" :data-state="status.state" :title="title" :aria-label="clickable || to ? `${text}: show compile attempts` : undefined"
		class="inline-block max-w-full shrink-0 truncate whitespace-nowrap rounded-full px-2 py-0.5 align-middle text-[11px] font-semibold"
		:class="[TONES[status.tone], clickable || to ? 'hover:underline' : '']" @click="clickable && !to && $emit('open')">
		{{ text }}
	</component>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { RouterLink, type RouteLocationRaw } from "vue-router";
import {
	compiledStatus,
	compiledVersion,
	sourceAttempt,
	type TaskCompileAttempt,
} from "../../lib/compiledStatus";
import { age } from "../../lib/projects";
import type { AgentTask } from "../../stores/settings";

// A precompiling task's compiled status: compiled, stale, not compiled,
// compile failed, falling back to the model, or compiling. One chip for the
// task and workflow editors, the task page header, and cards.

const props = defineProps<{
	task: AgentTask;
	/** The task's compile attempts, newest first, when they are loaded. */
	attempts?: TaskCompileAttempt[];
	/** The prompt as edited, so an edit marks the script stale. */
	editedPrompt?: string;
	/** Adds the script's version and when it was compiled. */
	detail?: boolean;
	/** Links the chip to where its compile attempts show. */
	to?: RouteLocationRaw;
	/** Makes the chip a button that emits open, to show its compile attempts. */
	clickable?: boolean;
}>();
defineEmits<{ open: [] }>();

const TONES = {
	green: "bg-green-50 text-green-700 dark:bg-green-950/50 dark:text-green-300",
	amber: "bg-amber-50 text-amber-800 dark:bg-amber-950/50 dark:text-amber-300",
	red: "bg-red-50 text-red-700 dark:bg-red-950/50 dark:text-red-300",
	gray: "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-300",
} as const;

const status = computed(() =>
	compiledStatus(props.task, {
		editedPrompt: props.editedPrompt,
		attempts: props.attempts,
	}),
);

const version = computed(() => compiledVersion(props.task));
const compiledAt = computed(
	() => sourceAttempt(props.task, props.attempts)?.created_at ?? "",
);

const text = computed(() => {
	const s = status.value;
	if (!s) return "";
	if (!props.detail || !props.task.compiled?.trim()) return s.label;
	const parts = [s.label];
	if (version.value) parts.push(`v${version.value}`);
	if (compiledAt.value) parts.push(`${age(compiledAt.value)} ago`);
	return parts.join(" · ");
});

const title = computed(() => {
	const s = status.value;
	if (!s) return "";
	const at = compiledAt.value
		? ` Compiled ${new Date(compiledAt.value).toLocaleString()}.`
		: "";
	return `${s.title}${at}`;
});
</script>
