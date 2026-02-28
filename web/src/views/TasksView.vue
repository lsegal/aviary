<template>
  <AppLayout>
    <div class="px-6 py-6">
      <div class="mb-6 flex items-center justify-between">
        <h2 class="text-xl font-bold text-white">Jobs</h2>
        <button
          class="rounded-lg bg-gray-800 px-4 py-2 text-sm text-gray-300 hover:bg-gray-700"
          @click="store.fetchJobs()"
        >Refresh</button>
      </div>

      <div v-if="store.loading" class="text-gray-400 text-sm">Loading…</div>
      <div v-else-if="!store.jobs.length" class="text-gray-500 text-sm">No jobs yet.</div>
      <table v-else class="w-full text-sm">
        <thead>
          <tr class="border-b border-gray-800 text-left text-xs font-medium text-gray-400">
            <th class="pb-2 pr-4">ID</th>
            <th class="pb-2 pr-4">Task</th>
            <th class="pb-2 pr-4">Agent</th>
            <th class="pb-2 pr-4">Status</th>
            <th class="pb-2">Attempts</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="job in store.jobs"
            :key="job.id"
            class="border-b border-gray-800/50 text-gray-300"
          >
            <td class="py-2 pr-4 font-mono text-xs text-gray-500">{{ job.id.slice(-8) }}</td>
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
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import AppLayout from '../components/AppLayout.vue'
import { useTasksStore } from '../stores/tasks'

const store = useTasksStore()
onMounted(() => store.fetchJobs())

function statusClass(status: string): string {
  const map: Record<string, string> = {
    pending: 'bg-gray-700 text-gray-300',
    in_progress: 'bg-blue-900 text-blue-300',
    completed: 'bg-green-900 text-green-300',
    failed: 'bg-red-900 text-red-300',
  }
  return map[status] ?? 'bg-gray-800 text-gray-400'
}
</script>
