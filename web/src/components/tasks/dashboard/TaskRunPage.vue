<template>
	<div class="space-y-3" data-testid="task-run-page">
		<router-link :to="taskSessionPath(agent, task)" data-testid="task-run-back"
			class="inline-flex items-center gap-1 text-sm font-medium text-blue-600 hover:underline dark:text-blue-400">
			<ArrowLeftIcon aria-hidden="true" class="h-4 w-4" /> Back to {{ task }}
		</router-link>
		<TaskRunLog v-if="entry" :entry="entry" :title="`Run ${entry.id}`" :now="dashboard.now.value"
			:fetch-logs="dashboard.fetchLogs" class="h-[calc(100dvh-16rem)] min-h-80" @stop="dashboard.stop($event)" />
		<p v-else-if="dashboard.loaded.value" data-testid="task-run-missing"
			class="rounded-xl border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
			No run <span class="font-mono">{{ runID }}</span> for this task.
		</p>
		<p v-else class="text-sm text-gray-500 dark:text-gray-400">Loading run…</p>
	</div>
</template>

<script setup lang="ts">
import ArrowLeftIcon from "@heroicons/vue/24/outline/ArrowLeftIcon";
import { computed } from "vue";
import { useRoute } from "vue-router";
import type { TaskDashboard } from "../../../composables/useTaskDashboard";
import { taskSessionPath } from "../../../stores/settings";
import TaskRunLog from "./TaskRunLog.vue";

// One run of a task at its own URL: its full logs, with a failed run's error
// on top. It opens inside the task's dashboard, so the chat stays mounted.

const props = defineProps<{
	dashboard: TaskDashboard;
	agent: string;
	task: string;
}>();

const route = useRoute();
const runID = computed(() =>
	typeof route.params.run === "string" ? route.params.run : "",
);
const entry = computed(() =>
	props.dashboard.entries.value.find((e) => e.id === runID.value),
);
</script>
