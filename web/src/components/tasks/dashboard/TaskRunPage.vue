<template>
	<div class="space-y-3" data-testid="task-run-page">
		<template v-if="entry">
			<TaskRunLog :entry="entry" :title="`Run ${entry.id}`" :now="dashboard.now.value"
				:fetch-logs="dashboard.fetchLogs" class="h-[calc(100dvh-16rem)] min-h-80" @stop="dashboard.stop($event)" />
			<TriggerContextPanel v-if="entry.job?.trigger_context" :context="entry.job.trigger_context" />
		</template>
		<p v-else-if="dashboard.loaded.value" data-testid="task-run-missing"
			class="rounded-xl border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
			No run <span class="font-mono">{{ runID }}</span> for this task.
		</p>
		<p v-else class="text-sm text-gray-500 dark:text-gray-400">Loading run…</p>
	</div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import type { TaskDashboard } from "../../../composables/useTaskDashboard";
import TaskRunLog from "./TaskRunLog.vue";
import TriggerContextPanel from "./TriggerContextPanel.vue";

// One run of a task at its own URL: its full logs, with a failed run's error
// on top. It opens inside the task's dashboard, so the chat stays mounted;
// the page header's back link returns to the task (#1064).

const props = defineProps<{
	dashboard: TaskDashboard;
}>();

const route = useRoute();
const runID = computed(() =>
	typeof route.params.run === "string" ? route.params.run : "",
);
const entry = computed(() =>
	props.dashboard.entries.value.find((e) => e.id === runID.value),
);
</script>
