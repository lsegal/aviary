<template>
  <AppLayout>
    <div class="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-6 sm:px-6">
      <!-- Filters -->
      <div class="mb-6 flex flex-wrap items-center gap-3">
        <div class="flex overflow-hidden rounded-lg border border-gray-200 text-sm dark:border-gray-700">
          <button
            v-for="p in presets"
            :key="p.days"
            class="px-3 py-1.5 transition-colors"
            :class="activePreset === p.days
              ? 'bg-blue-600 text-white'
              : 'bg-white text-gray-600 hover:bg-gray-100 dark:bg-gray-900 dark:text-gray-400 dark:hover:bg-gray-800'"
            @click="applyPreset(p.days)">
            {{ p.label }}
          </button>
        </div>
        <div class="flex flex-wrap items-center gap-1 text-sm">
          <input v-model="store.startDate" type="date"
            class="rounded-lg border border-gray-200 bg-white px-2 py-1.5 text-gray-700 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300"
            @change="activePreset = null; store.applyFilters()" />
          <span class="text-gray-400">to</span>
          <input v-model="store.endDate" type="date"
            class="rounded-lg border border-gray-200 bg-white px-2 py-1.5 text-gray-700 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300"
            @change="activePreset = null; store.applyFilters()" />
        </div>
        <span class="ml-auto text-sm font-semibold text-gray-700 dark:text-gray-300">
          {{ fmtTokens(store.totalTokens) }} tokens<template v-if="store.totalCost !== null">
            · <span data-testid="usage-total-cost">{{ fmtCost(store.totalCost) }}</span></template>
        </span>
        <button
          class="rounded-lg bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
          :disabled="store.loading" @click="store.fetch()">
          {{ store.loading ? "Loading..." : "Refresh" }}
        </button>
        <SystemSettingsLink />
      </div>

      <div v-if="store.error"
        class="mb-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-400">
        {{ store.error }}
      </div>

      <!-- Overview stat cards -->
      <div class="mb-6 grid gap-3 sm:grid-cols-2 md:grid-cols-4 lg:grid-cols-8">
        <div v-for="card in statCards" :key="card.label"
          class="flex flex-col rounded-xl border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-900">
          <span class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">{{ card.label }}</span>
          <span class="text-xl font-bold" :class="card.color ?? 'text-gray-900 dark:text-white'">{{ card.value }}</span>
        </div>
      </div>

      <!-- Projects -->
      <div class="mb-6 rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900"
        data-testid="usage-projects">
        <h3 class="mb-4 text-sm font-semibold text-gray-700 dark:text-gray-300">Code Loop</h3>
        <div class="grid gap-3 sm:grid-cols-3 md:grid-cols-6">
          <div v-for="card in projectCards" :key="card.label" class="flex flex-col">
            <span class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">{{ card.label }}</span>
            <span class="text-xl font-bold" :class="card.color ?? 'text-gray-900 dark:text-white'"
              :data-testid="card.testid">{{ card.value }}</span>
          </div>
        </div>
        <div v-if="store.projectBreakdown.length" class="mt-4 overflow-x-auto">
          <table class="w-full text-xs" data-testid="usage-projects-breakdown">
            <thead>
              <tr class="border-b border-gray-100 text-left text-[10px] uppercase tracking-wide text-gray-400 dark:border-gray-800 dark:text-gray-500">
                <th class="py-2 pr-3 font-semibold">Project</th>
                <th class="px-3 py-2 text-right font-semibold">Worked On</th>
                <th class="px-3 py-2 text-right font-semibold">Merged</th>
                <th class="px-3 py-2 text-right font-semibold">Failed</th>
                <th class="px-3 py-2 text-right font-semibold">Wall Time</th>
                <th class="py-2 pl-3 text-right font-semibold">Code Changed</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in store.projectBreakdown" :key="row.name"
                class="border-b border-gray-50 last:border-0 dark:border-gray-800/50" data-testid="usage-project-row">
                <td class="py-2 pr-3 font-mono text-gray-700 dark:text-gray-300" data-testid="usage-project-name">{{ row.name }}</td>
                <td class="px-3 py-2 text-right text-gray-600 dark:text-gray-400">{{ row.issues }}</td>
                <td class="px-3 py-2 text-right text-gray-600 dark:text-gray-400">{{ row.merged }}</td>
                <td class="px-3 py-2 text-right text-gray-600 dark:text-gray-400">{{ row.failed }}</td>
                <td class="px-3 py-2 text-right text-gray-600 dark:text-gray-400">{{ fmtDuration(row.wallMs) }}</td>
                <td class="py-2 pl-3 text-right font-mono" data-testid="usage-project-lines">
                  <span class="text-green-600 dark:text-green-400">+{{ fmtLines(row.additions) }}</span> <span
                    class="text-red-500">−{{ fmtLines(row.deletions) }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Top breakdowns -->
      <div class="mb-6 grid gap-4 lg:grid-cols-3">
        <div v-for="breakdown in breakdowns" :key="breakdown.title"
          class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
          <h3 class="mb-4 text-sm font-semibold text-gray-700 dark:text-gray-300">{{ breakdown.title }}</h3>
          <p v-if="!breakdown.items.length" class="text-xs text-gray-400">No data</p>
          <div v-for="item in breakdown.items" :key="item.name" class="mb-3">
            <div class="mb-1 flex items-center justify-between text-xs">
              <span class="max-w-[70%] truncate text-gray-600 dark:text-gray-400">{{ item.name }}</span>
              <span class="font-mono text-gray-500">
                {{ fmtTokens(item.tokens) }}<template v-if="item.cost !== null">
                  · <span data-testid="usage-item-cost">{{ fmtCost(item.cost) }}</span></template>
              </span>
            </div>
            <div class="h-1.5 w-full overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
              <div class="h-full rounded-full bg-blue-400 transition-all dark:bg-blue-600"
                :style="{ width: pct(item.tokens, breakdown.maxTokens) }" />
            </div>
          </div>
        </div>
      </div>

      <!-- Activity by Time + Daily chart -->
      <div class="mb-6 grid gap-4 xl:grid-cols-2">
        <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
          <div class="mb-4 flex items-center justify-between">
            <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Activity by Time</h3>
            <span class="text-xs text-gray-400">{{ fmtTokens(store.totalTokens) }} tokens</span>
          </div>

          <p class="mb-1 text-xs font-medium text-gray-400 dark:text-gray-500">Day of Week</p>
          <div class="mb-1 flex items-end gap-1" style="height:64px">
            <div v-for="(val, idx) in store.byDayOfWeek" :key="idx"
              class="group relative flex flex-1 flex-col items-center">
              <div class="w-full cursor-default rounded-t-sm transition-colors"
                :class="hoveredDay === idx ? 'bg-blue-500' : 'bg-blue-200 dark:bg-blue-900 hover:bg-blue-400 dark:hover:bg-blue-700'"
                :style="{ height: barHeight(val, store.byDayOfWeek) }"
                @mouseenter="hoveredDay = idx"
                @mouseleave="hoveredDay = null" />
              <div v-if="hoveredDay === idx"
                class="pointer-events-none absolute -top-8 z-10 whitespace-nowrap rounded bg-gray-900 px-2 py-0.5 text-xs text-white dark:bg-gray-700">
                {{ fmtTokens(val) }}
              </div>
            </div>
          </div>
          <div class="mb-5 flex text-[10px] text-gray-400">
            <span v-for="d in dayLabels" :key="d" class="flex-1 text-center">{{ d }}</span>
          </div>

          <p class="mb-1 text-xs font-medium text-gray-400 dark:text-gray-500">Hours</p>
          <div class="flex flex-wrap gap-0.5">
            <div v-for="(val, h) in store.byHour" :key="h"
              class="group relative h-5 w-5 cursor-default rounded-sm transition-colors"
              :class="heatClass(val, store.byHour)"
              @mouseenter="hoveredHour = h"
              @mouseleave="hoveredHour = null">
              <div v-if="hoveredHour === h"
                class="pointer-events-none absolute bottom-full left-1/2 z-10 mb-1 -translate-x-1/2 whitespace-nowrap rounded bg-gray-900 px-2 py-0.5 text-xs text-white dark:bg-gray-700">
                {{ h }}:00 - {{ fmtTokens(val) }}
              </div>
            </div>
          </div>
          <div class="mt-2 flex items-center gap-1.5 text-[10px] text-gray-400">
            <span>Low</span>
            <div class="h-2 w-16 rounded-full bg-linear-to-r from-gray-200 to-blue-500 dark:from-gray-800" />
            <span>High token density</span>
          </div>
        </div>

        <!-- Daily stacked bar chart -->
        <div class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
          <div class="mb-3 flex items-center justify-between">
            <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Daily Token Usage</h3>
            <div class="flex items-center gap-3 text-[10px] text-gray-500">
              <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded-sm bg-blue-500" /> Input</span>
              <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded-sm bg-orange-400" /> Output</span>
              <span class="flex items-center gap-1"><span class="inline-block h-2 w-2 rounded-sm bg-cyan-400" /> Cache</span>
            </div>
          </div>
          <div v-if="!store.byDay.length" class="flex h-32 items-center justify-center text-xs text-gray-400">
            No data in selected range
          </div>
          <div v-else>
            <div class="flex items-end gap-px overflow-hidden" style="height:80px">
              <div v-for="(row, i) in store.byDay" :key="row.date"
                class="group relative flex flex-1 cursor-default flex-col justify-end"
                style="height:80px"
                @mouseenter="hoveredDayChart = i"
                @mouseleave="hoveredDayChart = null">
                <div v-if="hoveredDayChart === i"
                  class="pointer-events-none absolute bottom-full left-1/2 z-10 mb-1 -translate-x-1/2 whitespace-nowrap rounded bg-gray-900 px-2 py-1 text-[10px] text-white shadow dark:bg-gray-700">
                  <div class="font-semibold">{{ row.date }}</div>
                  <div>In: {{ fmtTokens(row.input) }}</div>
                  <div>Out: {{ fmtTokens(row.output) }}</div>
                  <div v-if="row.cache > 0">Cache: {{ fmtTokens(row.cache) }}</div>
                </div>
                <div v-if="sH(row,'cache',store.byDay) > 0" class="w-full bg-cyan-400 dark:bg-cyan-600" :style="{ height: sH(row,'cache',store.byDay)+'px' }" />
                <div v-if="sH(row,'output',store.byDay) > 0" class="w-full bg-orange-400 dark:bg-orange-600" :style="{ height: sH(row,'output',store.byDay)+'px' }" />
                <div v-if="sH(row,'input',store.byDay) > 0" class="w-full bg-blue-500 dark:bg-blue-700" :style="{ height: sH(row,'input',store.byDay)+'px' }" />
              </div>
            </div>
            <div class="mt-1 flex overflow-hidden">
              <span v-for="(row, i) in store.byDay" :key="row.date"
                class="flex-1 truncate text-center text-[9px] text-gray-400">
                {{ showLabel(i, store.byDay.length) ? row.date.slice(5) : "" }}
              </span>
            </div>
          </div>
        </div>
      </div>

      <!-- Tokens by type banner -->
      <div class="mb-6 rounded-xl border border-gray-200 bg-white p-4 dark:border-gray-800 dark:bg-gray-900">
        <div class="mb-2 flex items-center gap-3">
          <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Tokens by Type</h3>
          <span class="text-xs text-gray-400">Total {{ fmtTokens(grandTotal) }}</span>
        </div>
        <div class="flex h-4 overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
          <div class="bg-orange-400 transition-all" :style="{ width: pct(store.totalOutput, grandTotal) }" />
          <div class="bg-blue-500 transition-all" :style="{ width: pct(store.totalInput, grandTotal) }" />
          <div class="bg-cyan-400 transition-all" :style="{ width: pct(store.totalCacheRead, grandTotal) }" />
          <div class="bg-teal-500 transition-all" :style="{ width: pct(store.totalCacheWrite, grandTotal) }" />
        </div>
        <div class="mt-2 flex flex-wrap gap-3 text-xs text-gray-500 dark:text-gray-400">
          <span><span class="mr-1 inline-block h-2 w-2 rounded-sm bg-orange-400" />Output {{ fmtTokens(store.totalOutput) }}</span>
          <span><span class="mr-1 inline-block h-2 w-2 rounded-sm bg-blue-500" />Input {{ fmtTokens(store.totalInput) }}</span>
          <span><span class="mr-1 inline-block h-2 w-2 rounded-sm bg-cyan-400" />Cache Read {{ fmtTokens(store.totalCacheRead) }}</span>
          <span><span class="mr-1 inline-block h-2 w-2 rounded-sm bg-teal-500" />Cache Write {{ fmtTokens(store.totalCacheWrite) }}</span>
        </div>
      </div>

      <!-- Sessions list -->
      <div class="rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900" data-testid="usage-sessions">
        <div class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-gray-100 px-4 py-3 sm:px-5 dark:border-gray-800">
          <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Sessions</h3>
          <span class="text-xs text-gray-400" data-testid="usage-session-total">{{ fmtCount(store.sessionTotal) }} {{ store.sessionTotal === 1 ? "session" : "sessions" }}</span>
          <div class="grid w-full grid-cols-2 gap-2 sm:ml-auto sm:flex sm:w-auto sm:flex-wrap">
            <AppSelect v-model="store.agentFilter" size="sm" class="min-w-0 sm:w-36" aria-label="Agent"
              data-testid="usage-filter-agent" :options="agentFilterOptions" @update:model-value="store.applyFilters()" />
            <AppSelect v-model="store.modelFilter" size="sm" class="min-w-0 sm:w-44" aria-label="Model"
              data-testid="usage-filter-model" :options="modelFilterOptions" @update:model-value="store.applyFilters()" />
            <AppSelect v-model="store.statusFilter" size="sm" class="min-w-0 sm:w-32" aria-label="Status"
              data-testid="usage-filter-status" :options="STATUS_OPTIONS" @update:model-value="store.applyFilters()" />
            <AppSelect v-model="store.deletedFilter" size="sm" class="min-w-0 sm:w-40" aria-label="Deleted sessions"
              data-testid="usage-filter-deleted" :options="DELETED_OPTIONS" @update:model-value="store.applyFilters()" />
          </div>
        </div>
        <div v-if="!store.sessions.length" class="px-5 py-8 text-center text-sm text-gray-400">
          No sessions match in this date range.
        </div>
        <template v-else>
          <!-- Desktop table -->
          <table class="hidden w-full text-left text-xs sm:table">
            <thead>
              <tr class="border-b border-gray-100 text-gray-400 dark:border-gray-800">
                <th v-for="col in SESSION_COLUMNS" :key="col.key" scope="col"
                  :aria-sort="store.sort === col.key ? (store.order === 'asc' ? 'ascending' : 'descending') : 'none'"
                  class="py-2.5 font-medium first:pl-5 last:pr-5" :class="[col.right ? 'text-right' : '', col.key === 'name' ? 'px-4' : 'px-4']">
                  <button type="button" class="inline-flex items-center gap-1 hover:text-gray-700 dark:hover:text-gray-200"
                    :data-testid="`usage-sort-${col.key}`" @click="store.sortBy(col.key)">
                    {{ col.label }}
                    <ChevronUpIcon v-if="store.sort === col.key && store.order === 'asc'" class="h-3 w-3" />
                    <ChevronDownIcon v-else-if="store.sort === col.key" class="h-3 w-3" />
                  </button>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in store.sessions" :key="`${s.agent_id}/${s.session_id}`" data-testid="usage-session-row"
                class="border-b border-gray-50 hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-gray-800/50">
                <td class="max-w-0 py-2.5 pr-4 pl-5" style="width: 40%">
                  <span class="flex min-w-0 items-center gap-1.5">
                    <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="statusDot(s)" />
                    <TrashIcon v-if="s.deleted" class="h-3.5 w-3.5 shrink-0 text-gray-400" data-testid="usage-session-deleted"
                      :title="deletedTooltip(s)" aria-hidden="true" />
                    <span class="flex min-w-0 flex-col">
                      <RouterLink v-if="sessionLink(s)" :to="sessionLink(s)!" data-testid="usage-session-name"
                        class="truncate text-gray-700 hover:text-blue-600 hover:underline dark:text-gray-300">{{ s.name }}</RouterLink>
                      <span v-else data-testid="usage-session-name" :title="s.deleted ? deletedTooltip(s) : undefined"
                        class="truncate" :class="nameClass(s)">{{ s.name }}</span>
                      <span class="truncate text-[10px] text-gray-400" data-testid="usage-session-kind">{{ kindLabel(s) }}</span>
                    </span>
                  </span>
                </td>
                <td class="truncate px-4 py-2.5 text-gray-600 dark:text-gray-400">{{ s.agent_id }}</td>
                <td class="px-4 py-2.5">
                  <span class="rounded-full bg-gray-100 px-2 py-0.5 text-gray-600 dark:bg-gray-800 dark:text-gray-400">{{ sessionModel(s) }}</span>
                </td>
                <td class="px-4 py-2.5 text-right font-mono text-gray-700 dark:text-gray-300">{{ fmtTokens(s.input + s.output) }}</td>
                <td class="px-4 py-2.5 text-right text-gray-600 dark:text-gray-400">{{ s.tool_calls }}</td>
                <td class="py-2.5 pr-5 pl-4 whitespace-nowrap text-gray-400">{{ fmtTs(s.last_ts) }}</td>
              </tr>
            </tbody>
          </table>

          <!-- Phone cards -->
          <ul class="divide-y divide-gray-100 sm:hidden dark:divide-gray-800">
            <li v-for="s in store.sessions" :key="`${s.agent_id}/${s.session_id}`" class="px-4 py-3" data-testid="usage-session-card">
              <div class="flex min-w-0 items-center gap-1.5">
                <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="statusDot(s)" />
                <TrashIcon v-if="s.deleted" class="h-3.5 w-3.5 shrink-0 text-gray-400" :title="deletedTooltip(s)" aria-hidden="true" />
                <RouterLink v-if="sessionLink(s)" :to="sessionLink(s)!"
                  class="truncate text-sm text-gray-700 hover:text-blue-600 dark:text-gray-300">{{ s.name }}</RouterLink>
                <span v-else class="truncate text-sm" :class="nameClass(s)" :title="s.deleted ? deletedTooltip(s) : undefined">{{ s.name }}</span>
                <span class="ml-auto shrink-0 font-mono text-xs text-gray-700 dark:text-gray-300">{{ fmtTokens(s.input + s.output) }}</span>
              </div>
              <div class="mt-1 truncate text-[11px] text-gray-400">
                {{ kindLabel(s) }} · {{ s.agent_id }} · {{ sessionModel(s) }} · {{ s.tool_calls }} {{ s.tool_calls === 1 ? "tool" : "tools" }} · {{ fmtTs(s.last_ts) }}
              </div>
            </li>
          </ul>

          <!-- Pages -->
          <div class="flex items-center justify-between gap-2 px-4 py-3 text-xs text-gray-500 sm:px-5 dark:text-gray-400">
            <span data-testid="usage-session-range">{{ pageStart }}–{{ pageEnd }} of {{ fmtCount(store.sessionTotal) }}</span>
            <div class="flex items-center gap-1">
              <button type="button" class="rounded-md border border-gray-200 px-2.5 py-1 hover:bg-gray-100 disabled:opacity-40 dark:border-gray-700 dark:hover:bg-gray-800"
                :disabled="currentPage === 0 || store.sessionsLoading" data-testid="usage-page-prev" @click="store.goToPage(currentPage - 1)">
                Prev
              </button>
              <span class="px-2" data-testid="usage-page">Page {{ currentPage + 1 }} of {{ pageCount }}</span>
              <button type="button" class="rounded-md border border-gray-200 px-2.5 py-1 hover:bg-gray-100 disabled:opacity-40 dark:border-gray-700 dark:hover:bg-gray-800"
                :disabled="currentPage + 1 >= pageCount || store.sessionsLoading" data-testid="usage-page-next" @click="store.goToPage(currentPage + 1)">
                Next
              </button>
            </div>
          </div>
        </template>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import ChevronDownIcon from "@heroicons/vue/16/solid/ChevronDownIcon";
import ChevronUpIcon from "@heroicons/vue/16/solid/ChevronUpIcon";
import TrashIcon from "@heroicons/vue/16/solid/TrashIcon";
import { computed, onMounted, ref } from "vue";
import AppLayout from "../components/AppLayout.vue";
import SystemSettingsLink from "../components/SystemSettingsLink.vue";
import { AppSelect, type SelectOption } from "../components/ui/select";
import {
	blankKeyLabel,
	type DeletedFilter,
	SESSIONS_PAGE_SIZE,
	type SessionSort,
	type StatusFilter,
	type UsageSession,
	useUsageStore,
} from "../stores/usage";

const store = useUsageStore();

const SESSION_COLUMNS: { key: SessionSort; label: string; right?: boolean }[] =
	[
		{ key: "name", label: "Session" },
		{ key: "agent", label: "Agent" },
		{ key: "model", label: "Model" },
		{ key: "tokens", label: "Tokens", right: true },
		{ key: "tools", label: "Tools", right: true },
		{ key: "last", label: "Last Activity" },
	];

const STATUS_OPTIONS: SelectOption<StatusFilter>[] = [
	{ value: "", label: "Any status" },
	{ value: "ok", label: "OK" },
	{ value: "error", label: "Error" },
];

const DELETED_OPTIONS: SelectOption<DeletedFilter>[] = [
	{ value: "include", label: "With deleted" },
	{ value: "exclude", label: "Without deleted" },
	{ value: "only", label: "Only deleted" },
];

const agentFilterOptions = computed<SelectOption[]>(() => [
	{ value: "", label: "All agents" },
	...withSelected(store.agentOptions, store.agentFilter).map((a) => ({
		value: a,
		label: a,
	})),
]);

const modelFilterOptions = computed<SelectOption[]>(() => [
	{ value: "", label: "All models" },
	...withSelected(store.modelOptions, store.modelFilter).map((m) => ({
		value: m,
		label: m,
	})),
]);

// A filter stays choosable after a date change leaves it no records.
function withSelected(list: string[], selected: string): string[] {
	return selected && !list.includes(selected) ? [selected, ...list] : list;
}

const pageCount = computed(() =>
	Math.max(1, Math.ceil(store.sessionTotal / SESSIONS_PAGE_SIZE)),
);
const currentPage = computed(() =>
	Math.floor(store.offset / SESSIONS_PAGE_SIZE),
);
const pageStart = computed(() => (store.sessionTotal ? store.offset + 1 : 0));
const pageEnd = computed(() => store.offset + store.sessions.length);

const KIND_LABELS: Record<UsageSession["kind"], string> = {
	chat: "Chat",
	job: "Code Loop job",
	task: "Task",
	workflow: "Workflow",
};

function kindLabel(s: UsageSession): string {
	const kind = KIND_LABELS[s.kind] ?? s.kind;
	// A task's owner is its own "agent/task" key, which the name repeats.
	return s.owner && s.kind !== "task" ? `${kind} · ${s.owner}` : kind;
}

function statusDot(s: UsageSession): string {
	if (s.has_error) return "bg-red-500";
	return s.has_throttle ? "bg-amber-500" : "bg-green-500";
}

function nameClass(s: UsageSession): string {
	return s.deleted
		? "text-gray-400 line-through dark:text-gray-500"
		: "text-gray-700 dark:text-gray-300";
}

function deletedTooltip(s: UsageSession): string {
	if (!s.deleted_at) return "Session deleted";
	const d = new Date(s.deleted_at);
	return `Session deleted on ${d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" })} ${d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}`;
}

// Live chats open in Chat; deleted sessions and job runs have nowhere to go.
function sessionLink(s: UsageSession): string | null {
	if (s.deleted || s.kind !== "chat") return null;
	return `/agents/${encodeURIComponent(s.agent_id)}/chat/${encodeURIComponent(s.session_id)}`;
}

// The server names models provider/model; a blank one gets the breakdowns' label.
function sessionModel(s: UsageSession): string {
	return s.model || blankKeyLabel(s);
}

function fmtCount(n: number): string {
	return n.toLocaleString("en-US");
}

const hoveredDay = ref<number | null>(null);
const hoveredHour = ref<number | null>(null);
const hoveredDayChart = ref<number | null>(null);
const activePreset = ref<number | null>(7);

const presets = [
	{ label: "Today", days: 0 },
	{ label: "7d", days: 7 },
	{ label: "30d", days: 30 },
];
const dayLabels = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const grandTotal = computed(
	() =>
		store.totalInput +
		store.totalOutput +
		store.totalCacheRead +
		store.totalCacheWrite,
);

const statCards = computed(() => [
	...(store.totalCost !== null
		? [{ label: "Cost", value: fmtCost(store.totalCost) }]
		: []),
	{ label: "Messages", value: String(store.totalMessages) },
	{ label: "Tool Calls", value: String(store.totalToolCalls) },
	{
		label: "Throttles",
		value: String(store.totalThrottles),
		color:
			store.totalThrottles > 0
				? "text-amber-500"
				: "text-gray-900 dark:text-white",
	},
	{
		label: "Errors",
		value: String(store.totalErrors),
		color:
			store.totalErrors > 0 ? "text-red-500" : "text-gray-900 dark:text-white",
	},
	{ label: "Avg Tokens/Msg", value: fmtTokens(store.avgTokensPerMsg) },
	{ label: "Sessions", value: String(store.sessionCount) },
	{ label: "Input Tokens", value: fmtTokens(store.totalInput) },
	{ label: "Output Tokens", value: fmtTokens(store.totalOutput) },
	{
		label: "Throttle Rate",
		value: `${store.throttleRate.toFixed(1)}%`,
		color:
			store.throttleRate > 5
				? "text-amber-500"
				: store.throttleRate === 0
					? "text-green-600 dark:text-green-400"
					: "text-gray-900 dark:text-white",
	},
	{
		label: "Error Rate",
		value: `${store.errorRate.toFixed(1)}%`,
		color:
			store.errorRate > 5
				? "text-red-500"
				: store.errorRate === 0
					? "text-green-600 dark:text-green-400"
					: "text-gray-900 dark:text-white",
	},
]);

const projectCards = computed(() => {
	const p = store.projectSummary;
	return [
		{
			label: "Issues Worked On",
			value: String(p.issues),
			testid: "usage-projects-issues",
		},
		{
			label: "Merged",
			value: String(p.merged),
			testid: "usage-projects-merged",
			color: p.merged > 0 ? "text-green-600 dark:text-green-400" : undefined,
		},
		{
			label: "Failed",
			value: String(p.failed),
			testid: "usage-projects-failed",
			color: p.failed > 0 ? "text-red-500" : undefined,
		},
		{
			label: "Wall Time",
			value: fmtDuration(p.wallMs),
			testid: "usage-projects-wall-time",
		},
		{
			label: "Lines Added",
			value: `+${fmtLines(p.additions)}`,
			testid: "usage-projects-additions",
			color: "text-green-600 dark:text-green-400",
		},
		{
			label: "Lines Removed",
			value: `−${fmtLines(p.deletions)}`,
			testid: "usage-projects-deletions",
			color: "text-red-500",
		},
	];
});

const breakdowns = computed(() => [
	{
		title: "Top Models",
		items: store.topModels,
		maxTokens: Math.max(...store.topModels.map((m) => m.tokens), 1),
	},
	{
		title: "Top Providers",
		items: store.topProviders,
		maxTokens: Math.max(...store.topProviders.map((m) => m.tokens), 1),
	},
	{
		title: "Top Agents",
		items: store.topAgents,
		maxTokens: Math.max(...store.topAgents.map((m) => m.tokens), 1),
	},
]);

function applyPreset(days: number) {
	activePreset.value = days;
	store.setPreset(days);
}

function fmtTokens(n: number): string {
	if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
	if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
	return String(n);
}

function fmtCost(n: number): string {
	return `$${n.toFixed(n < 0.01 ? 4 : 2)}`;
}

function fmtLines(n: number): string {
	return n.toLocaleString("en-US");
}

function fmtDuration(ms: number): string {
	const minutes = Math.floor(ms / 60_000);
	if (minutes < 1) return `${Math.floor(ms / 1000)}s`;
	const hours = Math.floor(minutes / 60);
	if (hours < 1) return `${minutes}m`;
	return `${hours}h ${minutes % 60}m`;
}

function fmtTs(ts: string): string {
	if (!ts) return "-";
	const d = new Date(ts);
	return (
		d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) +
		" " +
		d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
	);
}

function pct(part: number, total: number): string {
	if (!total) return "0%";
	return `${Math.round((part / total) * 100)}%`;
}

function barHeight(val: number, arr: number[]): string {
	const max = Math.max(...arr, 1);
	return `${Math.max(2, Math.round((val / max) * 56))}px`;
}

const heatBuckets = [
	"bg-gray-100 dark:bg-gray-800",
	"bg-blue-100 dark:bg-blue-950",
	"bg-blue-200 dark:bg-blue-900",
	"bg-blue-400 dark:bg-blue-700",
	"bg-blue-600",
	"bg-red-500",
];
function heatClass(val: number, arr: number[]): string {
	const max = Math.max(...arr, 1);
	const ratio = val / max;
	const idx = Math.min(
		heatBuckets.length - 1,
		Math.floor(ratio * heatBuckets.length),
	);
	return heatBuckets[idx];
}

function sH(
	row: { input: number; output: number; cache: number },
	field: "input" | "output" | "cache",
	allRows: { input: number; output: number; cache: number }[],
): number {
	const maxTotal = Math.max(
		...allRows.map((r) => r.input + r.output + r.cache),
		1,
	);
	const total = row.input + row.output + row.cache;
	if (!total) return 0;
	const totalH = Math.max(2, Math.round((total / maxTotal) * 80));
	const outputH = Math.round((row.output / total) * totalH);
	const cacheH = Math.round((row.cache / total) * totalH);
	const inputH = totalH - outputH - cacheH;
	return field === "input" ? inputH : field === "output" ? outputH : cacheH;
}

function showLabel(i: number, len: number): boolean {
	if (len <= 8) return true;
	const step = Math.max(1, Math.ceil(len / 8));
	return i === 0 || i === len - 1 || i % step === 0;
}

onMounted(() => store.fetch());
</script>
