<template>
  <AppLayout>
    <div class="px-6 py-6">
      <div class="mb-6 flex items-center justify-between">
        <h2 class="text-xl font-bold text-gray-900 dark:text-white">Tasks</h2>
        <button
          class="rounded-lg bg-gray-100 px-4 py-2 text-sm text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700"
          @click="refreshAll">Refresh</button>
      </div>

      <div v-if="store.runError" class="mt-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-400">
        {{ store.runError }}
      </div>
      <div v-else-if="store.lastStartedJob" class="mt-4 rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 text-sm text-blue-700 dark:border-blue-900 dark:bg-blue-950 dark:text-blue-300">
        Started {{ store.lastStartedJob.task_id }} as {{ store.lastStartedJob.id.slice(-8) }}.
      </div>

      <section class="mt-8">
        <h3 class="mb-3 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Configured Tasks</h3>
        <div class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
          <div v-if="store.tasksLoading" class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">Loading tasks…</div>
          <div v-else-if="!store.tasks.length" class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">No configured tasks found.</div>
          <table v-else class="w-full text-sm">
            <thead>
              <tr
                class="border-b border-gray-200 text-left text-xs font-medium text-gray-500 dark:border-gray-800 dark:text-gray-400">
                <th class="px-4 py-2">Agent</th>
                <th class="px-4 py-2">Task</th>
                <th class="px-4 py-2">Trigger Type</th>
                <th class="px-4 py-2">Trigger</th>
                <th class="px-4 py-2">Target</th>
                <th class="px-4 py-2">Task Type</th>
                <th class="px-4 py-2">Content</th>
                <th class="px-4 py-2 text-right">Action</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="task in store.tasks" :key="task.id"
                class="border-b border-gray-100 text-gray-700 dark:border-gray-800/50 dark:text-gray-300">
                <td class="px-4 py-2">{{ task.agent_name }}</td>
                <td class="px-4 py-2">{{ task.name }}</td>
                <td class="px-4 py-2 uppercase text-xs font-semibold text-gray-500 dark:text-gray-400">{{ task.trigger_type }}</td>
                <td class="px-4 py-2 font-mono text-xs text-gray-500 dark:text-gray-400">{{ describeTrigger(task) }}</td>
                <td class="px-4 py-2">{{ task.target || "—" }}</td>
                <td class="px-4 py-2 uppercase text-xs font-semibold text-gray-500 dark:text-gray-400">{{ task.type || "prompt" }}</td>
                <td class="max-w-lg truncate px-4 py-2" :title="taskBody(task)">{{ taskBody(task) || "—" }}</td>
                <td class="px-4 py-2 text-right">
                  <button
                    class="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
                    :disabled="store.runningTaskID === task.id"
                    @click="runTask(task.id)"
                  >
                    {{ store.runningTaskID === task.id ? "Running…" : "Run Now" }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="mt-8">
        <h3 class="mb-3 text-sm font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">Job History</h3>
        <div v-if="store.loading" class="text-gray-500 text-sm dark:text-gray-400">Loading jobs…</div>
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
              <td class="py-2 pr-4">{{ job.agent_id }}</td>
              <td class="py-2 pr-4">
                <span :class="statusClass(job.status)" class="rounded-full px-2 py-0.5 text-xs font-medium">
                  {{ job.status }}
                </span>
              </td>
              <td class="py-2">{{ job.attempts }}</td>
            </tr>
          </tbody>
        </table>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted } from "vue";
import AppLayout from "../components/AppLayout.vue";
import type { ScheduledTask } from "../stores/tasks";
import { useTasksStore } from "../stores/tasks";

const store = useTasksStore();

onMounted(() => {
	refreshAll();
});

async function runTask(taskID: string) {
	if (!taskID) return;
	await store.runTask(taskID);
}

function refreshAll() {
	store.fetchTasks();
	store.fetchJobs();
}

function describeTrigger(task: ScheduledTask): string {
	const parts: string[] = [];
	if (task.schedule) parts.push(`schedule: ${task.schedule}`);
	if (task.watch) parts.push(`watch: ${task.watch}`);
	if (task.start_at) parts.push(`start_at: ${task.start_at}`);
	if (task.run_once) parts.push("run_once");
	return parts.length ? parts.join(" | ") : "—";
}

function taskBody(task: ScheduledTask): string {
	return task.prompt || "";
}

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
