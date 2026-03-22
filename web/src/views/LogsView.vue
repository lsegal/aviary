<template>
  <AppLayout>
    <div class="flex h-full flex-col overflow-hidden">
      <!-- Filter bar -->
      <div class="shrink-0 border-b border-gray-200 bg-white px-4 py-3 dark:border-gray-800 dark:bg-gray-900">
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-sm font-semibold text-gray-700 dark:text-gray-300">Logs</span>

          <!-- Connection dot -->
          <span class="h-2 w-2 shrink-0 rounded-full"
            :class="logs.connected.value ? 'bg-green-500' : 'bg-red-400 animate-pulse'"
            :title="logs.connected.value ? 'Live' : 'Reconnecting…'" />

          <div class="mx-1 h-4 w-px bg-gray-200 dark:bg-gray-700" />

          <!-- Level filter -->
          <select v-model="logs.filterLevel.value"
            class="rounded-md border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-gray-700 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300 focus:outline-none">
            <option value="debug">DEBUG+</option>
            <option value="info">INFO+</option>
            <option value="warn">WARN+</option>
            <option value="error">ERROR</option>
          </select>

          <!-- Component chips -->
          <div class="flex flex-wrap gap-1">
            <button v-for="comp in logs.allComponents.value" :key="comp"
              class="rounded-full px-2.5 py-0.5 text-xs font-medium transition-colors"
              :class="isComponentActive(comp)
                ? componentActiveClass(comp)
                : 'border border-gray-200 bg-white text-gray-500 hover:border-gray-400 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-400 dark:hover:border-gray-500'"
              @click="logs.toggleComponent(comp)">
              {{ comp }}
            </button>
          </div>

          <!-- Text search -->
          <input v-model="logs.filterText.value" type="search" placeholder="Filter…"
            class="ml-auto rounded-md border border-gray-200 bg-gray-50 px-2 py-1 text-xs text-gray-700 placeholder-gray-400 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300 dark:placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-blue-400"
            style="min-width: 120px; max-width: 200px" />

          <!-- Auto-scroll toggle -->
          <button class="rounded-md border px-2 py-1 text-xs font-medium transition-colors" :class="autoScroll
            ? 'border-blue-300 bg-blue-50 text-blue-700 dark:border-blue-700 dark:bg-blue-950 dark:text-blue-300'
            : 'border-gray-200 bg-white text-gray-500 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-400'"
            @click="autoScroll = !autoScroll">
            {{ autoScroll ? '↓ Auto' : '| Paused' }}
          </button>

          <!-- Clear -->
          <button
            class="rounded-md border border-gray-200 px-2 py-1 text-xs text-gray-500 hover:border-red-300 hover:text-red-600 dark:border-gray-700 dark:text-gray-400 dark:hover:border-red-700 dark:hover:text-red-400"
            @click="logs.clearLogs()">
            Clear
          </button>
        </div>
      </div>

      <!-- Log list -->
      <div ref="scrollEl" class="flex-1 overflow-y-auto bg-gray-950 p-3 font-mono text-xs leading-5" @scroll="onScroll">
        <!-- Load previous button -->
        <div v-if="logs.hasMore.value" class="flex justify-center py-2">
          <button
            class="rounded-md border border-gray-700 bg-gray-800 px-3 py-1 text-xs text-gray-400 hover:border-gray-500 hover:text-gray-200 disabled:opacity-50"
            :disabled="logs.loadingMore.value" @click="logs.loadPrevious()">
            {{ logs.loadingMore.value ? 'Loading…' : `↑ Load previous ${logs.historyPageSize} lines` }}
          </button>
        </div>

        <div v-if="logs.filtered.value.length === 0" class="py-8 text-center text-gray-600">
          No log entries yet.
        </div>

        <div v-for="entry in logs.filtered.value" :key="entry.seq"
          class="flex gap-2 border-b border-gray-800/40 py-0.5 hover:bg-gray-900">
          <!-- Timestamp -->
          <span class="shrink-0 text-gray-600 tabular-nums">{{ fmtTime(entry.ts) }}</span>

          <!-- Level badge -->
          <span class="shrink-0 w-10 text-center rounded text-xs font-semibold uppercase tracking-wide"
            :class="levelClass(entry.level)">{{ entry.level }}</span>

          <!-- Component -->
          <span class="shrink-0 rounded px-1 text-xs"
            :class="componentLabelClass(entry.component)">{{ entry.component }}</span>

          <!-- Message + attrs -->
          <span class="min-w-0 break-words text-gray-200">
            <template v-if="isLong(entry) && !expanded.has(entry.seq)">
              {{ entryText(entry).slice(0, 2000) }}<button
                class="ml-1.5 shrink-0 rounded-full border border-gray-600 bg-gray-800 px-1.5 py-0 text-xs text-gray-400 hover:border-gray-400 hover:text-gray-200"
                @click="expanded.add(entry.seq); expanded = new Set(expanded)">+{{ entryText(entry).length - 2000 }}
                bytes</button>
            </template>
            <template v-else>
              {{ entry.msg }}
              <span v-for="(val, key) in entry.attrs" :key="key" class="ml-1 text-gray-500">{{ key }}=<span
                  class="text-gray-400">{{ val }}</span></span>
              <button v-if="isLong(entry)"
                class="ml-1.5 shrink-0 rounded-full border border-gray-600 bg-gray-800 px-1.5 py-0 text-xs text-gray-400 hover:border-gray-400 hover:text-gray-200"
                @click="expanded.delete(entry.seq); expanded = new Set(expanded)">collapse</button>
            </template>
          </span>
        </div>

        <!-- Spacer so the last row isn't flush with the bottom -->
        <div class="h-4" />
      </div>

      <!-- Error banner -->
      <div v-if="logs.error.value"
        class="shrink-0 border-t border-yellow-700 bg-yellow-950 px-4 py-2 text-xs text-yellow-300">
        {{ logs.error.value }}
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from "vue";
import AppLayout from "../components/AppLayout.vue";
import { useLogs } from "../composables/useLogs";

const logs = useLogs();
const scrollEl = ref<HTMLElement | null>(null);
const autoScroll = ref(true);
const expanded = ref(new Set<number>());

type LogEntry = (typeof logs.filtered.value)[number];

const LONG_THRESHOLD = 2000;

function entryText(entry: LogEntry): string {
	let text = entry.msg;
	for (const [key, val] of Object.entries(entry.attrs ?? {})) {
		text += ` ${key}=${val}`;
	}
	return text;
}

function isLong(entry: LogEntry): boolean {
	return entryText(entry).length > LONG_THRESHOLD;
}

// Scroll to bottom when new entries arrive (only when autoScroll is on).
watch(
	() => logs.filtered.value.length,
	async () => {
		if (!autoScroll.value) return;
		await nextTick();
		if (scrollEl.value) {
			scrollEl.value.scrollTop = scrollEl.value.scrollHeight;
		}
	},
);

// Pause auto-scroll if user scrolls up.
function onScroll() {
	if (!scrollEl.value) return;
	const { scrollTop, scrollHeight, clientHeight } = scrollEl.value;
	autoScroll.value = scrollTop + clientHeight >= scrollHeight - 32;
}

// Time formatting: show HH:MM:SS.mmm
function fmtTime(iso: string): string {
	try {
		const d = new Date(iso);
		const hh = d.getHours().toString().padStart(2, "0");
		const mm = d.getMinutes().toString().padStart(2, "0");
		const ss = d.getSeconds().toString().padStart(2, "0");
		const ms = d.getMilliseconds().toString().padStart(3, "0");
		return `${hh}:${mm}:${ss}.${ms}`;
	} catch {
		return iso;
	}
}

function levelClass(level: string): string {
	const map: Record<string, string> = {
		debug: "text-gray-500",
		info: "text-blue-400",
		warn: "text-yellow-400",
		error: "text-red-400",
	};
	return map[level] ?? "text-gray-400";
}

// Colour palette for component chips (active state).
const COMP_PALETTE = [
	"border-blue-500 bg-blue-900/40 text-blue-300",
	"border-emerald-500 bg-emerald-900/40 text-emerald-300",
	"border-violet-500 bg-violet-900/40 text-violet-300",
	"border-amber-500 bg-amber-900/40 text-amber-300",
	"border-pink-500 bg-pink-900/40 text-pink-300",
	"border-teal-500 bg-teal-900/40 text-teal-300",
	"border-orange-500 bg-orange-900/40 text-orange-300",
	"border-cyan-500 bg-cyan-900/40 text-cyan-300",
];

function compIndex(comp: string): number {
	// Stable index by hashing the component name.
	let h = 0;
	for (let i = 0; i < comp.length; i++) h = (h * 31 + comp.charCodeAt(i)) >>> 0;
	return h % COMP_PALETTE.length;
}

function componentActiveClass(comp: string): string {
	return `border ${COMP_PALETTE[compIndex(comp)]}`;
}

function componentLabelClass(comp: string): string {
	const colors = [
		"text-blue-400",
		"text-emerald-400",
		"text-violet-400",
		"text-amber-400",
		"text-pink-400",
		"text-teal-400",
		"text-orange-400",
		"text-cyan-400",
	];
	return colors[compIndex(comp)];
}

function isComponentActive(comp: string): boolean {
	return logs.filterComponents.value.has(comp);
}
</script>
