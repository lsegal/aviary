<template>
  <AppLayout>
    <div class="px-6 py-6">
      <div class="mb-6 flex items-center justify-between">
        <h2 class="text-xl font-bold text-gray-900 dark:text-white">Jobs</h2>
        <button
          class="rounded-lg bg-gray-100 px-4 py-2 text-sm text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700"
          @click="store.fetchJobs()">Refresh</button>
      </div>

      <div v-if="store.loading" class="text-gray-500 text-sm dark:text-gray-400">Loading…</div>
      <div v-else-if="!store.jobs.length" class="text-gray-500 text-sm">No jobs yet.</div>
      <table v-else class="w-full text-sm">
        <thead>
          <tr
            class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-gray-800 dark:text-gray-400">
            <th class="pb-2 pr-4">ID</th>
            <th class="pb-2 pr-4">Task</th>
            <th class="pb-2 pr-4">Agent</th>
            <th class="pb-2 pr-4">Status</th>
            <th class="pb-2">Attempts</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="job in store.jobs" :key="job.id"
            class="border-b border-gray-100 text-gray-700 dark:border-gray-800/50 dark:text-gray-300">
            <td class="py-2 pr-4 font-mono text-xs text-gray-400 dark:text-gray-500">{{ job.id.slice(-8) }}</td>
            <td class="py-2 pr-4">{{ job.task_id }}</td>
            <td class="py-2 pr-4">{{ job.agent_name }}</td>
            <td class="py-2 pr-4">
              <span :class="statusClass(job.status)" class="rounded-full px-2 py-0.5 text-xs font-medium">
                {{ job.status }}
              </span>
            </td>
            <td class="py-2">{{ job.attempts }}</td>
          </tr>
        </tbody>
      </table>

      <section class="mt-8">
        <h3 class="mb-3 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Task Config</h3>
        <div class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
          <div v-if="!configuredTasks.length" class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">No task config
            found.</div>
          <table v-else class="w-full text-sm">
            <thead>
              <tr
                class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-gray-800 dark:text-gray-400">
                <th class="px-4 py-2">Agent</th>
                <th class="px-4 py-2">Task</th>
                <th class="px-4 py-2">Trigger</th>
                <th class="px-4 py-2">Channel</th>
                <th class="px-4 py-2">Prompt</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="task in configuredTasks" :key="`${task.agent}:${task.name}:${task.trigger}`"
                class="border-b border-gray-100 text-gray-700 dark:border-gray-800/50 dark:text-gray-300">
                <td class="px-4 py-2">{{ task.agent }}</td>
                <td class="px-4 py-2">{{ task.name }}</td>
                <td class="px-4 py-2 font-mono text-xs text-gray-500 dark:text-gray-400">{{ task.trigger }}</td>
                <td class="px-4 py-2">{{ task.channel }}</td>
                <td class="max-w-lg truncate px-4 py-2" :title="task.prompt">{{ task.prompt }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted } from "vue";
import { useSettingsStore } from "../stores/settings";
import { useTasksStore } from "../stores/tasks";

const store = useTasksStore();
const settingsStore = useSettingsStore();

const configuredTasks = computed(() =>
	(settingsStore.config?.agents ?? []).flatMap((agent) =>
		(agent.tasks ?? []).map((task) => ({
			agent: agent.name,
			name: task.name || "—",
			trigger: (() => {
				const parts: string[] = [];
				if (task.schedule) parts.push(`schedule: ${task.schedule}`);
				if (task.watch) parts.push(`watch: ${task.watch}`);
				if (task.start_at) parts.push(`start_at: ${task.start_at}`);
				if (task.run_once) parts.push("run_once");
				return parts.length ? parts.join(" | ") : "—";
			})(),
			channel: task.channel || "—",
			prompt: task.prompt || "",
		})),
	),
);

onMounted(() => {
	store.fetchJobs();
	settingsStore.fetchConfig();
});

function statusClass(status: string): string {
	const map: Record<string, string> = {
		pending: "bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300",
		in_progress:
			"bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
		completed:
			"bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
		failed: "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
	};
	return (
		map[status] ??
		"bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400"
	);
}
</script>
