<template>
  <AppLayout>
    <!-- Initial load -->
    <div v-if="!store.fetched" class="flex h-full items-center justify-center">
      <svg class="h-6 w-6 animate-spin text-gray-400 dark:text-gray-500" fill="none" viewBox="0 0 24 24"
        stroke="currentColor" stroke-width="2">
        <path stroke-linecap="round" stroke-linejoin="round"
          d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
      </svg>
    </div>

    <!-- Setup wizard: shown until at least one agent exists -->
    <div v-else-if="showWizard" class="h-full overflow-y-auto">
      <SetupWizard @skip="dismissed = true" />
    </div>

    <div v-else class="px-6 py-6">
      <div class="mb-6 flex items-center justify-between">
        <h2 class="text-xl font-bold text-gray-900 dark:text-white">Overview</h2>
        <button
          class="rounded-lg bg-gray-100 px-4 py-2 text-sm text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700"
          :disabled="store.loading" @click="store.fetchAll()">
          {{ store.loading ? 'Loading…' : 'Refresh' }}
        </button>
      </div>

      <div v-if="store.error"
        class="mb-6 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-400">
        {{ store.error }}
      </div>

      <!-- Stat cards -->
      <div class="mb-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <!-- Agents -->
        <router-link to="/agents"
          class="flex flex-col rounded-xl border border-gray-200 bg-white p-5 hover:border-blue-300 hover:shadow-sm dark:border-gray-800 dark:bg-gray-900 dark:hover:border-blue-700">
          <span
            class="mb-3 text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">Agents</span>
          <span class="text-3xl font-bold text-gray-900 dark:text-white">{{ store.agents.length }}</span>
          <div class="mt-2 flex flex-wrap gap-1">
            <span v-for="(count, state) in agentStateCounts" :key="state" :class="agentStateBadge(state)"
              class="rounded-full px-2 py-0.5 text-xs font-medium">{{ count }} {{ state }}</span>
            <span v-if="!store.agents.length" class="text-xs text-gray-400 dark:text-gray-500">none configured</span>
          </div>
        </router-link>

        <!-- Jobs -->
        <router-link to="/tasks"
          class="flex flex-col rounded-xl border border-gray-200 bg-white p-5 hover:border-blue-300 hover:shadow-sm dark:border-gray-800 dark:bg-gray-900 dark:hover:border-blue-700">
          <span class="mb-3 text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">Jobs</span>
          <span class="text-3xl font-bold text-gray-900 dark:text-white">{{ store.jobs.length }}</span>
          <div class="mt-2 flex flex-wrap gap-1">
            <span v-if="inProgressJobs > 0"
              class="rounded-full bg-blue-100 px-2 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-300">{{ inProgressJobs }}
              running</span>
            <span v-if="failedJobs > 0"
              class="rounded-full bg-red-100 px-2 py-0.5 text-xs font-medium text-red-700 dark:bg-red-900 dark:text-red-300">{{ failedJobs }}
              failed</span>
            <span v-if="store.jobs.length === 0" class="text-xs text-gray-400 dark:text-gray-500">no jobs yet</span>
          </div>
        </router-link>

        <!-- Sessions -->
        <router-link to="/sessions"
          class="flex flex-col rounded-xl border border-gray-200 bg-white p-5 hover:border-blue-300 hover:shadow-sm dark:border-gray-800 dark:bg-gray-900 dark:hover:border-blue-700">
          <span
            class="mb-3 text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">Sessions</span>
          <span class="text-3xl font-bold text-gray-900 dark:text-white">—</span>
          <span class="mt-2 text-xs text-gray-400 dark:text-gray-500">history available in Sessions</span>
        </router-link>

        <!-- Config health -->
        <div class="flex flex-col rounded-xl border p-5" :class="healthCardClass">
          <span class="mb-3 text-xs font-semibold uppercase tracking-wide" :class="healthLabelClass">Config
            Health</span>
          <div class="flex items-center gap-2">
            <svg v-if="errorCount > 0 || warnCount > 0" class="h-5 w-5 shrink-0" :class="healthTextClass" fill="none"
              viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
            </svg>
            <svg v-else class="h-5 w-5 shrink-0" :class="healthTextClass" fill="none" viewBox="0 0 24 24"
              stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
            </svg>
            <span class="text-lg font-bold" :class="healthTextClass">{{ healthLabel }}</span>
          </div>
          <p class="mt-2 text-xs" :class="healthSubClass">
            <template v-if="errorCount > 0 || warnCount > 0">
              <span v-if="errorCount > 0">{{ errorCount }} error{{ errorCount !== 1 ? 's' : '' }}</span>
              <span v-if="errorCount > 0 && warnCount > 0">, </span>
              <span v-if="warnCount > 0">{{ warnCount }} warning{{ warnCount !== 1 ? 's' : '' }}</span>
            </template>
            <template v-else>all checks passed</template>
          </p>
        </div>
      </div>

      <!-- Doctor panel -->
      <div class="rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
        <!-- Header -->
        <div class="flex items-center justify-between border-b border-gray-100 px-5 py-4 dark:border-gray-800">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">Config Validation</h3>
          <div class="flex items-center gap-3">
            <span v-if="store.lastChecked" class="text-xs text-gray-400 dark:text-gray-500">
              checked {{ timeAgo(store.lastChecked) }}
            </span>
            <button
              class="flex items-center gap-1.5 rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-200 disabled:opacity-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700"
              :disabled="store.loading" @click="store.recheck()">
              <svg class="h-3 w-3" :class="{ 'animate-spin': store.loading }" fill="none" viewBox="0 0 24 24"
                stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round"
                  d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              Re-check
            </button>
          </div>
        </div>

        <!-- Loading -->
        <div v-if="store.loading" class="flex items-center gap-3 px-5 py-8 text-sm text-gray-400 dark:text-gray-500">
          <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round"
              d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          Running checks…
        </div>

        <!-- All clear -->
        <div v-else-if="store.issues.length === 0" class="flex items-center gap-3 px-5 py-8">
          <span
            class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/40">
            <svg class="h-4 w-4 text-green-600 dark:text-green-400" fill="none" viewBox="0 0 24 24"
              stroke="currentColor" stroke-width="2.5">
              <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
            </svg>
          </span>
          <div>
            <p class="text-sm font-medium text-gray-900 dark:text-white">No issues found</p>
            <p class="text-xs text-gray-400 dark:text-gray-500">Configuration and credentials look good.</p>
          </div>
        </div>

        <!-- Issues grouped by level -->
        <div v-else>
          <!-- Errors -->
          <div v-if="errorCount > 0">
            <div class="flex items-center gap-2 bg-red-50 px-5 py-2.5 dark:bg-red-950/20">
              <svg class="h-3.5 w-3.5 text-red-500 dark:text-red-400" fill="none" viewBox="0 0 24 24"
                stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round"
                  d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
              </svg>
              <span class="text-xs font-semibold text-red-600 dark:text-red-400">{{ errorCount }} Error{{ errorCount !==
                1 ?
                's' : '' }}</span>
            </div>
            <ul class="divide-y divide-gray-50 dark:divide-gray-800/50">
              <li v-for="(issue, i) in store.issues.filter(x => x.level === 'ERROR')" :key="'e' + i"
                class="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-x-4 px-5 py-3">
                <code class="truncate text-xs font-mono text-gray-500 dark:text-gray-400">{{ issue.field }}</code>
                <p class="text-xs text-gray-700 dark:text-gray-300">{{ issue.message }}</p>
              </li>
            </ul>
          </div>

          <!-- Warnings -->
          <div v-if="warnCount > 0">
            <div
              class="flex items-center gap-2 border-t border-gray-100 bg-yellow-50 px-5 py-2.5 dark:border-gray-800 dark:bg-yellow-950/20">
              <svg class="h-3.5 w-3.5 text-yellow-500 dark:text-yellow-400" fill="none" viewBox="0 0 24 24"
                stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round"
                  d="M12 9v3.75m9.303 3.376c.816 1.3.258 3.374-1.948 3.374H4.645c-2.206 0-2.764-2.074-1.948-3.374l6.657-10.748c1.083-1.75 3.51-1.75 4.593 0l6.656 10.748zM12 15.75h.007v.008H12v-.008z" />
              </svg>
              <span class="text-xs font-semibold text-yellow-600 dark:text-yellow-400">{{ warnCount }} Warning{{
                warnCount !==
                  1 ? 's' : '' }}</span>
            </div>
            <ul class="divide-y divide-gray-50 dark:divide-gray-800/50">
              <li v-for="(issue, i) in store.issues.filter(x => x.level === 'WARN')" :key="'w' + i"
                class="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-x-4 px-5 py-3">
                <code class="truncate text-xs font-mono text-gray-500 dark:text-gray-400">{{ issue.field }}</code>
                <p class="text-xs text-gray-700 dark:text-gray-300">{{ issue.message }}</p>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import AppLayout from "../components/AppLayout.vue";
import SetupWizard from "../components/SetupWizard.vue";
import { useOverviewStore } from "../stores/overview";

const store = useOverviewStore();
onMounted(() => store.fetchAll());

const dismissed = ref(false);
const showWizard = computed(
	() =>
		store.fetched &&
		!store.error &&
		store.agents.length === 0 &&
		!dismissed.value,
);

// --- Agents ---
const agentStateCounts = computed(() => {
	const counts: Record<string, number> = {};
	for (const a of store.agents) {
		counts[a.state] = (counts[a.state] ?? 0) + 1;
	}
	return counts;
});

function agentStateBadge(state: string) {
	if (state === "idle")
		return "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300";
	if (state === "running")
		return "bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300";
	return "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400";
}

// --- Jobs ---
const inProgressJobs = computed(
	() => store.jobs.filter((j) => j.status === "in_progress").length,
);
const failedJobs = computed(
	() => store.jobs.filter((j) => j.status === "failed").length,
);

// --- Doctor ---
const errorCount = computed(
	() => store.issues.filter((i) => i.level === "ERROR").length,
);
const warnCount = computed(
	() => store.issues.filter((i) => i.level === "WARN").length,
);

const healthLabel = computed(() => {
	if (errorCount.value > 0) return "Errors";
	if (warnCount.value > 0) return "Warnings";
	return "Healthy";
});

const healthCardClass = computed(() => {
	if (errorCount.value > 0)
		return "border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/30";
	if (warnCount.value > 0)
		return "border-yellow-200 bg-yellow-50 dark:border-yellow-900 dark:bg-yellow-950/30";
	return "border-green-200 bg-green-50 dark:border-green-900 dark:bg-green-950/30";
});

const healthLabelClass = computed(() => {
	if (errorCount.value > 0) return "text-red-500 dark:text-red-400";
	if (warnCount.value > 0) return "text-yellow-600 dark:text-yellow-400";
	return "text-green-600 dark:text-green-400";
});

const healthTextClass = computed(() => {
	if (errorCount.value > 0) return "text-red-700 dark:text-red-300";
	if (warnCount.value > 0) return "text-yellow-700 dark:text-yellow-300";
	return "text-green-700 dark:text-green-300";
});

const healthSubClass = computed(() => {
	if (errorCount.value > 0) return "text-red-500 dark:text-red-400";
	if (warnCount.value > 0) return "text-yellow-600 dark:text-yellow-400";
	return "text-green-600 dark:text-green-500";
});

// --- Time ago ---
function timeAgo(date: Date): string {
	const secs = Math.floor((Date.now() - date.getTime()) / 1000);
	if (secs < 60) return `${secs}s ago`;
	const mins = Math.floor(secs / 60);
	if (mins < 60) return `${mins}m ago`;
	return `${Math.floor(mins / 60)}h ago`;
}
</script>
