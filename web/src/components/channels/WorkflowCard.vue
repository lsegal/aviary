<template>
	<router-link :to="taskSessionPath(agent, workflow.name)" data-testid="workflow-card" :data-workflow="workflow.name"
		:data-activity="status.activity" :data-paused="paused" :title="cardTitle" :aria-label="cardTitle"
		class="workflow-card group relative block shrink-0 rounded-xl p-[2px]" :class="[
			status.activity === 'idle' ? '' : 'workflow-card--active',
			compact ? 'h-10 w-10' : 'w-56',
		]" :style="{ '--snake': status.activity === 'running' ? RUNNING : TRIGGERING }">
		<div class="relative flex h-full items-center gap-2 rounded-[10px] border bg-white dark:bg-gray-900"
			:class="[paused ? 'border-dashed border-gray-300 opacity-70 dark:border-gray-700' : 'border-gray-200 dark:border-gray-800', compact ? 'justify-center' : 'px-3 py-2']">
			<BoltIcon aria-hidden="true" class="h-4 w-4 shrink-0"
				:class="paused ? 'text-gray-400' : status.last === 'failed' ? 'text-red-500' : 'text-blue-600 dark:text-blue-400'" />
			<template v-if="!compact">
				<div class="min-w-0 flex-1">
					<p class="truncate text-sm font-semibold text-gray-900 group-hover:underline dark:text-white">{{ workflow.name }}</p>
					<p class="truncate text-xs text-gray-500 dark:text-gray-400">{{ matchText }}</p>
				</div>
				<div class="flex shrink-0 flex-col items-end gap-1">
					<PausedChip v-if="paused" />
					<span v-else data-testid="workflow-card-state"
						class="rounded-full bg-green-50 px-2 py-0.5 text-xs font-semibold text-green-700 dark:bg-green-950/50 dark:text-green-300">
						{{ status.activity === "running" ? "Running" : status.activity === "triggering" ? "Triggered" : "Active" }}
					</span>
					<TaskPrecompileBadge :task="workflow" />
					<span v-if="status.last" data-testid="workflow-card-last" :data-last="status.last" class="text-[11px]"
						:class="status.last === 'failed' ? 'text-red-600 dark:text-red-400' : status.last === 'fallback' ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500 dark:text-gray-400'">
						{{ LAST_TEXT[status.last] }}
					</span>
				</div>
			</template>
			<span v-else-if="paused" class="absolute -right-1 -top-1 h-2.5 w-2.5 rounded-full bg-amber-400 ring-2 ring-white dark:ring-gray-950" />
		</div>
	</router-link>
</template>

<script setup lang="ts">
import BoltIcon from "@heroicons/vue/24/outline/BoltIcon";
import { computed } from "vue";
import type { WorkflowStatus } from "../../composables/useWorkflowStatus";
import { type AgentTask, taskSessionPath } from "../../stores/settings";
import PausedChip from "../tasks/PausedChip.vue";
import TaskPrecompileBadge from "../tasks/TaskPrecompileBadge.vue";

// A workflow as a card: active or paused, its match, and its last run. While
// it is triggering or running, a colored segment travels clockwise around the
// card's edge, in one color while triggering and another while running.

const props = defineProps<{
	agent: string;
	workflow: AgentTask;
	status: WorkflowStatus;
	compact?: boolean;
}>();

const LAST_TEXT = {
	succeeded: "last run ok",
	fallback: "last run fell back to the model",
	failed: "last run failed",
} as const;

const TRIGGERING = "#f59e0b";
const RUNNING = "#3b82f6";

const paused = computed(() => props.workflow.enabled === false);
const matchText = computed(() => {
	const m = props.workflow.match?.trim();
	const what = m && m !== "*" ? `matches ${m}` : "every message";
	return props.workflow.entry ? `${what} · ${props.workflow.entry}` : what;
});
const cardTitle = computed(() => {
	const state = paused.value
		? "paused"
		: props.status.activity === "idle"
			? "active"
			: props.status.activity;
	return `${props.workflow.name}: ${state}`;
});
</script>

<style scoped>
@property --snake-angle {
	syntax: "<angle>";
	inherits: false;
	initial-value: 0deg;
}

@property --snake {
	syntax: "<color>";
	inherits: true;
	initial-value: #f59e0b;
}

.workflow-card {
	transition: --snake 600ms ease;
}

/* The traveling segment: a short arc of a conic gradient behind the card's
	inner surface, rotated clockwise around it. */
.workflow-card--active::before {
	content: "";
	position: absolute;
	inset: 0;
	border-radius: inherit;
	background: conic-gradient(
		from var(--snake-angle),
		transparent 0deg 270deg,
		var(--snake) 330deg,
		transparent 360deg
	);
	animation: workflow-snake 1.6s linear infinite;
}

@keyframes workflow-snake {
	to {
		--snake-angle: 360deg;
	}
}

@media (prefers-reduced-motion: reduce) {
	.workflow-card {
		transition: none;
	}

	/* No travel and no tween: a static border in the state's color. */
	.workflow-card--active::before {
		animation: none;
		background: var(--snake);
	}
}
</style>
