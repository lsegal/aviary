<template>
  <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4 py-6"
    data-testid="upgrade-dialog" @click.self="$emit('close')">
    <div role="dialog" aria-modal="true" aria-labelledby="upgrade-dialog-title"
      class="flex max-h-[88vh] w-full max-w-lg flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-2xl dark:border-gray-800 dark:bg-gray-900">
      <div class="border-b border-gray-200 px-5 py-4 dark:border-gray-800">
        <h3 id="upgrade-dialog-title" class="text-sm font-semibold text-gray-900 dark:text-white">
          Upgrade Aviary to
          <a v-if="releaseUrl" :href="releaseUrl" target="_blank" rel="noreferrer"
            class="text-blue-600 underline hover:text-blue-500 dark:text-blue-400">{{ latestVersion }}</a>
          <span v-else>{{ latestVersion }}</span>
        </h3>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          Aviary installs the release, updates skills for every signed-in coding CLI, and restarts.
        </p>
      </div>

      <div class="space-y-3 overflow-y-auto px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
        <template v-if="jobs.length">
          <p>
            Upgrading now stops {{ jobs.length === 1 ? "this running job" : `these ${jobs.length} running jobs` }}
            and resumes each one in its own session after the restart:
          </p>
          <ul class="space-y-1.5" data-testid="upgrade-dialog-jobs">
            <li v-for="job in jobs" :key="`${job.source}:${job.id}`"
              class="rounded-lg border border-gray-200 px-3 py-2 dark:border-gray-700">
              <div class="font-medium text-gray-900 dark:text-white">{{ job.title || job.id }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">{{ job.source }} · {{ job.id }}</div>
            </li>
          </ul>
          <p class="text-xs text-gray-500 dark:text-gray-400">Waiting jobs aren't touched.</p>
        </template>
        <p v-else>No jobs are running, so nothing will be stopped.</p>
      </div>

      <div class="flex flex-wrap justify-end gap-2 border-t border-gray-200 px-5 py-3 dark:border-gray-800">
        <button type="button"
          class="rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-700 hover:bg-gray-100 dark:border-gray-700 dark:text-gray-300 dark:hover:bg-gray-800"
          @click="$emit('close')">Cancel</button>
        <button type="button" :disabled="busy"
          class="rounded-lg border border-yellow-400 px-3 py-1.5 text-xs font-semibold text-yellow-900 hover:bg-yellow-50 disabled:opacity-60 dark:text-yellow-200 dark:hover:bg-yellow-950/40"
          @click="$emit('confirm', 'idle')">When no jobs are running</button>
        <button type="button" :disabled="busy"
          class="rounded-lg bg-yellow-500 px-3 py-1.5 text-xs font-semibold text-yellow-950 hover:bg-yellow-400 disabled:opacity-60"
          @click="$emit('confirm', 'now')">{{ busy ? "Upgrading..." : "Now" }}</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { RestartJob, RestartWhen } from "../composables/useServerStatus";

defineProps<{
	open: boolean;
	latestVersion: string;
	releaseUrl: string;
	jobs: RestartJob[];
	busy: boolean;
}>();

defineEmits<{
	close: [];
	confirm: [when: RestartWhen];
}>();
</script>
