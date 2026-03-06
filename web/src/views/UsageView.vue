<template>
  <AppLayout>
    <div class="flex flex-1 flex-col overflow-y-auto px-6 py-6">
      <!-- Header + Filters -->
      <div class="mb-6 flex flex-wrap items-center gap-3">
        <h2 class="mr-2 text-xl font-bold text-gray-900 dark:text-white">Usage</h2>
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
        <div class="flex items-center gap-1 text-sm">
          <input v-model="store.startDate" type="date"
            class="rounded-lg border border-gray-200 bg-white px-2 py-1.5 text-gray-700 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300"
            @change="activePreset = null; store.fetch()" />
          <span class="text-gray-400">to</span>
          <input v-model="store.endDate" type="date"
            class="rounded-lg border border-gray-200 bg-white px-2 py-1.5 text-gray-700 dark:border-gray-700 dark:bg-gray-900 dark:text-gray-300"
            @change="activePreset = null; store.fetch()" />
        </div>
        <span class="ml-auto text-sm font-semibold text-gray-700 dark:text-gray-300">
          {{ fmtTokens(store.totalTokens) }} tokens
        </span>
        <button
          class="rounded-lg bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
          :disabled="store.loading" @click="store.fetch()">
          {{ store.loading ? "Loading..." : "Refresh" }}
        </button>
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

      <!-- Top breakdowns -->
      <div class="mb-6 grid gap-4 lg:grid-cols-3">
        <div v-for="breakdown in breakdowns" :key="breakdown.title"
          class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
          <h3 class="mb-4 text-sm font-semibold text-gray-700 dark:text-gray-300">{{ breakdown.title }}</h3>
          <p v-if="!breakdown.items.length" class="text-xs text-gray-400">No data</p>
          <div v-for="item in breakdown.items" :key="item.name" class="mb-3">
            <div class="mb-1 flex items-center justify-between text-xs">
              <span class="max-w-[70%] truncate text-gray-600 dark:text-gray-400">{{ item.name }}</span>
              <span class="font-mono text-gray-500">{{ fmtTokens(item.tokens) }}</span>
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
            <div class="h-2 w-16 rounded-full" style="background:linear-gradient(to right,#e5e7eb,#3b82f6)" />
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
      <div class="rounded-xl border border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900">
        <div class="flex items-center justify-between border-b border-gray-100 px-5 py-3 dark:border-gray-800">
          <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Sessions</h3>
          <span class="text-xs text-gray-400">{{ store.sessionList.length }} shown</span>
        </div>
        <div v-if="!store.sessionList.length" class="px-5 py-8 text-center text-sm text-gray-400">
          No sessions recorded in this date range.
        </div>
        <div v-else class="overflow-x-auto">
          <table class="w-full text-left text-xs">
            <thead>
              <tr class="border-b border-gray-100 text-gray-400 dark:border-gray-800">
                <th class="px-5 py-2.5 font-medium">Session</th>
                <th class="px-4 py-2.5 font-medium">Agent</th>
                <th class="px-4 py-2.5 font-medium">Model</th>
                <th class="px-4 py-2.5 text-right font-medium">Tokens</th>
                <th class="px-4 py-2.5 text-right font-medium">Tools</th>
                <th class="px-4 py-2.5 font-medium">Last Activity</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in store.sessionList" :key="s.session_id"
                class="border-b border-gray-50 hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-gray-800/50">
                <td class="px-5 py-2.5">
                  <span class="flex items-center gap-1.5">
                    <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="s.has_error ? 'bg-red-500' : 'bg-green-500'" />
                    <code class="font-mono text-gray-600 dark:text-gray-400">...{{ s.session_id.slice(-12) }}</code>
                  </span>
                </td>
                <td class="px-4 py-2.5 text-gray-600 dark:text-gray-400">{{ s.agent_name }}</td>
                <td class="px-4 py-2.5">
                  <span class="rounded-full bg-gray-100 px-2 py-0.5 text-gray-600 dark:bg-gray-800 dark:text-gray-400">{{ s.model }}</span>
                </td>
                <td class="px-4 py-2.5 text-right font-mono text-gray-700 dark:text-gray-300">{{ fmtTokens(s.input + s.output) }}</td>
                <td class="px-4 py-2.5 text-right text-gray-600 dark:text-gray-400">{{ s.tool_calls }}</td>
                <td class="px-4 py-2.5 text-gray-400">{{ fmtTs(s.last_ts) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useUsageStore } from "../stores/usage";

const store = useUsageStore();

const _hoveredDay = ref<number | null>(null);
const _hoveredHour = ref<number | null>(null);
const _hoveredDayChart = ref<number | null>(null);
const activePreset = ref<number | null>(7);

const _presets = [
	{ label: "Today", days: 0 },
	{ label: "7d", days: 7 },
	{ label: "30d", days: 30 },
];
const _dayLabels = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const _grandTotal = computed(
	() =>
		store.totalInput +
		store.totalOutput +
		store.totalCacheRead +
		store.totalCacheWrite,
);

const _statCards = computed(() => [
	{ label: "Messages", value: String(store.totalMessages) },
	{ label: "Tool Calls", value: String(store.totalToolCalls) },
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

const _breakdowns = computed(() => [
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

function _applyPreset(days: number) {
	activePreset.value = days;
	store.setPreset(days);
}

function fmtTokens(n: number): string {
	if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
	if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
	return String(n);
}

function _fmtTs(ts: string): string {
	if (!ts) return "-";
	const d = new Date(ts);
	return (
		d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) +
		" " +
		d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
	);
}

function _pct(part: number, total: number): string {
	if (!total) return "0%";
	return `${Math.round((part / total) * 100)}%`;
}

function _barHeight(val: number, arr: number[]): string {
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
function _heatClass(val: number, arr: number[]): string {
	const max = Math.max(...arr, 1);
	const ratio = val / max;
	const idx = Math.min(
		heatBuckets.length - 1,
		Math.floor(ratio * heatBuckets.length),
	);
	return heatBuckets[idx];
}

function _sH(
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

function _showLabel(i: number, len: number): boolean {
	if (len <= 8) return true;
	const step = Math.max(1, Math.ceil(len / 8));
	return i === 0 || i === len - 1 || i % step === 0;
}

onMounted(() => store.fetch());
</script>
