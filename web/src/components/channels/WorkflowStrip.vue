<template>
	<section v-if="workflows.length" aria-label="Workflows" data-testid="workflow-strip" :data-collapsed="collapsed"
		class="border-b border-gray-200 px-4 py-2 sm:px-6 dark:border-gray-800">
		<div class="flex items-center gap-3">
			<button type="button" data-testid="workflow-strip-toggle" :aria-expanded="!collapsed"
				class="inline-flex items-center gap-1 text-xs font-semibold uppercase tracking-wide text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200"
				@click="collapsed = !collapsed">
				<ChevronDownIcon aria-hidden="true" class="h-3.5 w-3.5 transition-transform" :class="collapsed ? '-rotate-90' : ''" />
				Workflows
				<span class="font-normal normal-case tracking-normal">({{ activeCount }} active)</span>
			</button>
			<label v-if="pausedCount" class="ml-auto inline-flex cursor-pointer items-center gap-1.5 text-xs text-gray-600 dark:text-gray-400">
				<input v-model="hidePaused" type="checkbox" data-testid="workflow-strip-hide-paused"
					class="h-3.5 w-3.5 rounded border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800" />
				Hide paused
			</label>
		</div>
		<div class="mt-2 flex gap-2 overflow-x-auto pb-1" :class="collapsed ? 'flex-nowrap' : 'flex-wrap'">
			<WorkflowCard v-for="w in shown" :key="w.name" :agent="agent" :workflow="w" :status="statusOf(w.name)"
				:compact="collapsed" />
		</div>
	</section>
</template>

<script setup lang="ts">
import ChevronDownIcon from "@heroicons/vue/24/outline/ChevronDownIcon";
import { computed, ref, watch } from "vue";
import type { WorkflowStatus } from "../../composables/useWorkflowStatus";
import type { AgentTask } from "../../stores/settings";
import WorkflowCard from "./WorkflowCard.vue";

// A channel's workflows as cards above its chat: active ones first, paused
// ones after or hidden, the strip collapsible to icons. Both choices are
// remembered.

const props = defineProps<{
	agent: string;
	workflows: AgentTask[];
	statusOf: (name: string) => WorkflowStatus;
}>();

const COLLAPSED_KEY = "aviary.channel.workflows.collapsed";
const HIDE_PAUSED_KEY = "aviary.channel.workflows.hidePaused";

function stored(key: string): boolean {
	try {
		return localStorage.getItem(key) === "1";
	} catch {
		return false;
	}
}
function store(key: string, v: boolean) {
	try {
		localStorage.setItem(key, v ? "1" : "0");
	} catch {
		// Storage unavailable: the choice lasts until reload.
	}
}

const collapsed = ref(stored(COLLAPSED_KEY));
const hidePaused = ref(stored(HIDE_PAUSED_KEY));
watch(collapsed, (v) => store(COLLAPSED_KEY, v));
watch(hidePaused, (v) => store(HIDE_PAUSED_KEY, v));

const isPaused = (w: AgentTask) => w.enabled === false;
const activeCount = computed(
	() => props.workflows.filter((w) => !isPaused(w)).length,
);
const pausedCount = computed(() => props.workflows.length - activeCount.value);

const shown = computed(() =>
	props.workflows
		.filter((w) => !(hidePaused.value && isPaused(w)))
		.slice()
		.sort(
			(a, b) =>
				Number(isPaused(a)) - Number(isPaused(b)) ||
				a.name.localeCompare(b.name),
		),
);
</script>
