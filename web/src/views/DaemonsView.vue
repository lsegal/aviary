<template>
  <AppLayout>
    <div class="flex h-full flex-col overflow-hidden">

      <!-- Header -->
      <div class="flex flex-shrink-0 items-center gap-3 border-b border-gray-200 px-6 py-4 dark:border-gray-800">
        <h2 class="mr-2 text-xl font-bold text-gray-900 dark:text-white">Daemons</h2>
        <span class="text-sm text-gray-500 dark:text-gray-400">{{ daemons.length }} process{{ daemons.length === 1 ? '' : 'es' }}</span>
        <div class="ml-auto flex items-center gap-3">
          <span v-if="lastRefresh" class="text-xs text-gray-400 dark:text-gray-500">
            Updated {{ timeSince(lastRefresh) }}
          </span>
          <button
            class="rounded-lg bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
            :disabled="loading" @click="fetchDaemons">
            {{ loading ? "Loading…" : "Refresh" }}
          </button>
        </div>
      </div>

      <!-- Error banner -->
      <div v-if="error"
        class="mx-6 mt-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-400">
        {{ error }}
      </div>

      <!-- Content -->
      <div class="flex-1 overflow-y-auto px-6 py-4">

        <!-- Empty state -->
        <div v-if="!loading && !daemons.length"
          class="flex h-40 items-center justify-center text-sm text-gray-400 dark:text-gray-500">
          No daemons running.
        </div>

        <!-- Daemon cards -->
        <div class="grid gap-4 xl:grid-cols-2">
          <div v-for="d in daemons" :key="d.name"
            class="rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">

            <!-- Card header -->
            <div class="mb-4 flex items-start justify-between">
              <div class="flex items-center gap-2.5">
                <span class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-sm font-bold"
                  :class="typeIconClass(d.type)">
                  {{ typeIcon(d.type) }}
                </span>
                <div>
                  <div class="flex items-center gap-2">
                    <span class="font-semibold text-gray-900 dark:text-white">{{ displayName(d) }}</span>
                    <span class="rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide"
                      :class="typeBadgeClass(d.type)">
                      {{ d.type }}
                    </span>
                    <span v-if="d.managed"
                      class="rounded-full bg-purple-100 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-purple-700 dark:bg-purple-900/40 dark:text-purple-400">
                      managed
                    </span>
                  </div>
                  <div class="mt-0.5 flex items-center gap-1.5 text-xs"
                    :class="statusTextClass(d.status)">
                    <span class="inline-block h-1.5 w-1.5 rounded-full"
                      :class="statusDotClass(d.status)" />
                    {{ capitalize(d.status) }} · {{ d.uptime }} uptime
                  </div>
                </div>
              </div>
              <button
                class="inline-flex shrink-0 items-center rounded-lg border border-gray-200 px-2.5 py-1 text-xs font-medium text-gray-600 transition hover:border-blue-300 hover:text-blue-600 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-700 dark:text-gray-300 dark:hover:border-blue-700 dark:hover:text-blue-400"
                :disabled="restarting.has(d.name) || !canRestart(d)"
                :title="restartTitle(d)"
                @click="restartDaemon(d)"
              >
                {{ restarting.has(d.name) ? "Restarting…" : "Restart" }}
              </button>
            </div>

            <!-- Stats grid -->
            <dl class="grid grid-cols-2 gap-x-6 gap-y-3 text-xs sm:grid-cols-3">
              <div v-if="d.pid">
                <dt class="mb-0.5 text-gray-400 dark:text-gray-500">PID</dt>
                <dd class="font-mono font-medium text-gray-700 dark:text-gray-300">{{ d.pid }}</dd>
              </div>
              <div v-if="d.addr">
                <dt class="mb-0.5 text-gray-400 dark:text-gray-500">Address</dt>
                <dd class="font-mono font-medium text-gray-700 dark:text-gray-300">{{ d.addr }}</dd>
              </div>
              <div>
                <dt class="mb-0.5 text-gray-400 dark:text-gray-500">Started</dt>
                <dd class="font-medium text-gray-700 dark:text-gray-300">{{ fmtDate(d.started) }}</dd>
              </div>
              <div v-if="d.rss_bytes">
                <dt class="mb-0.5 text-gray-400 dark:text-gray-500">Memory</dt>
                <dd class="font-mono font-medium text-gray-700 dark:text-gray-300">{{ fmtBytes(d.rss_bytes) }}</dd>
              </div>
              <div v-if="d.cpu_percent >= 0">
                <dt class="mb-0.5 text-gray-400 dark:text-gray-500">CPU</dt>
                <dd class="font-mono font-medium text-gray-700 dark:text-gray-300">{{ d.cpu_percent.toFixed(1) }}%</dd>
              </div>
              <div v-if="d.status">
                <dt class="mb-0.5 text-gray-400 dark:text-gray-500">Status</dt>
                <dd class="font-medium" :class="statusClass(d.status)">{{ d.status }}</dd>
              </div>
            </dl>

            <!-- Memory bar (only when available) -->
            <div v-if="d.rss_bytes && maxMem > 0" class="mt-4">
              <div class="mb-1 flex items-center justify-between text-[10px] text-gray-400 dark:text-gray-500">
                <span>Memory usage</span>
                <span>{{ fmtBytes(d.rss_bytes) }}</span>
              </div>
              <div class="h-1.5 w-full overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
                <div class="h-full rounded-full transition-all"
                  :class="memBarClass(d.rss_bytes)"
                  :style="{ width: memPct(d.rss_bytes) }" />
              </div>
            </div>

            <!-- Error message (only when channel has errored) -->
            <div v-if="d.error" class="mt-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-400">
              {{ d.error }}
            </div>

            <!-- Log tail (for all channel daemons, not the aviary server itself) -->
            <div v-if="d.type !== 'server'" class="mt-4">
              <button
                class="flex w-full items-center gap-1.5 text-left text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                @click="toggleLogs(d.name)">
                <span class="font-mono">{{ openLogs.has(d.name) ? '▾' : '▸' }}</span>
                <span>Logs</span>
                <span v-if="logLines[d.name]?.length" class="ml-auto text-gray-400 dark:text-gray-500">
                  {{ logLines[d.name].length }} line{{ logLines[d.name].length === 1 ? '' : 's' }}
                </span>
              </button>
              <div v-if="openLogs.has(d.name)"
                class="mt-2 h-48 overflow-y-auto rounded-lg bg-gray-950 p-3 font-mono text-[11px] leading-relaxed text-gray-300 dark:bg-black">
                <div v-if="!logLines[d.name]?.length" class="text-gray-600">No output yet…</div>
                <div v-for="(line, i) in logLines[d.name]" :key="i" class="whitespace-pre-wrap break-all">{{ line }}</div>
              </div>
            </div>

          </div>
        </div>

      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import AppLayout from "../components/AppLayout.vue";
import { useAuthStore } from "../stores/auth";

interface Daemon {
	name: string;
	type: string;
	pid?: number;
	addr?: string;
	started: string;
	uptime: string;
	cpu_percent: number;
	rss_bytes: number;
	status: string;
	error?: string;
	managed: boolean;
}

const auth = useAuthStore();
const daemons = ref<Daemon[]>([]);
const loading = ref(false);
const error = ref("");
const lastRefresh = ref<Date | null>(null);
const restarting = ref<Set<string>>(new Set());

// Log tail state: which daemons have logs open, their lines, and their SSE sources.
const openLogs = ref<Set<string>>(new Set());
const logLines = ref<Record<string, string[]>>({});
const logSources: Record<string, EventSource> = {};
const RETRYABLE_STATUSES = new Set([500, 502, 503, 504]);
const FETCH_RETRY_DELAYS_MS = [250, 750, 1500];

function toggleLogs(key: string) {
	if (openLogs.value.has(key)) {
		openLogs.value.delete(key);
		closeLogs(key);
	} else {
		openLogs.value.add(key);
		openLogs.value = new Set(openLogs.value); // trigger reactivity
		startLogStream(key);
	}
}

function startLogStream(key: string) {
	closeLogs(key); // close any existing stream
	if (!logLines.value[key]) {
		logLines.value[key] = [];
	}
	const token = auth.getToken() ?? "";
	const url = `/api/daemons/logs?key=${encodeURIComponent(key)}&token=${encodeURIComponent(token)}`;
	const es = new EventSource(url);
	es.onmessage = (ev) => {
		try {
			const line: string = JSON.parse(ev.data);
			if (!logLines.value[key]) logLines.value[key] = [];
			logLines.value[key].push(line);
			// Keep at most 500 lines in the UI.
			if (logLines.value[key].length > 500) {
				logLines.value[key] = logLines.value[key].slice(-500);
			}
			// Trigger reactivity.
			logLines.value = { ...logLines.value };
		} catch {
			// ignore parse errors
		}
	};
	es.onerror = () => {
		es.close();
	};
	logSources[key] = es;
}

function closeLogs(key: string) {
	if (logSources[key]) {
		logSources[key].close();
		delete logSources[key];
	}
}

let refreshTimer: ReturnType<typeof setInterval> | null = null;

const maxMem = computed(() =>
	Math.max(...daemons.value.map((d) => d.rss_bytes ?? 0), 1),
);

async function fetchDaemons() {
	loading.value = true;
	try {
		const data = await fetchDaemonsWithRetry();
		error.value = "";
		daemons.value = data;
		lastRefresh.value = new Date();
	} catch (e) {
		error.value = String(e);
	} finally {
		loading.value = false;
	}
}

async function fetchDaemonsWithRetry(): Promise<Daemon[]> {
	for (let attempt = 0; ; attempt += 1) {
		const res = await fetch("/api/daemons", {
			headers: { Authorization: `Bearer ${auth.getToken()}` },
		});
		if (res.ok) {
			return res.json();
		}
		if (
			RETRYABLE_STATUSES.has(res.status) &&
			attempt < FETCH_RETRY_DELAYS_MS.length
		) {
			await new Promise((resolve) =>
				setTimeout(resolve, FETCH_RETRY_DELAYS_MS[attempt]),
			);
			continue;
		}
		throw new Error(`HTTP ${res.status}`);
	}
}

function canRestart(d: Daemon): boolean {
	return d.name === "aviary" || d.managed;
}

function restartTitle(d: Daemon): string {
	if (d.name === "aviary") return "Restart Aviary";
	if (d.managed) return `Restart ${displayName(d)}`;
	return "Only Aviary-managed daemons can be restarted here";
}

async function restartDaemon(d: Daemon) {
	if (!canRestart(d) || restarting.value.has(d.name)) return;

	restarting.value.add(d.name);
	restarting.value = new Set(restarting.value);
	error.value = "";

	try {
		const res = await fetch("/api/daemons/restart", {
			method: "POST",
			headers: {
				Authorization: `Bearer ${auth.getToken()}`,
				"Content-Type": "application/json",
			},
			body: JSON.stringify({ key: d.name }),
		});
		if (!res.ok) {
			const message = await res.text();
			throw new Error(message || `HTTP ${res.status}`);
		}
		await fetchDaemons();
	} catch (e) {
		error.value = e instanceof Error ? e.message : String(e);
	} finally {
		restarting.value.delete(d.name);
		restarting.value = new Set(restarting.value);
	}
}

function displayName(d: Daemon): string {
	if (d.type === "server") return "Aviary Server";
	// key format: "agentName/type/index"
	const parts = d.name.split("/");
	if (parts.length === 3)
		return `${parts[0]} (${parts[1]} #${parseInt(parts[2], 10) + 1})`;
	return d.name;
}

function typeIcon(type: string): string {
	return { server: "⚙", signal: "📡", slack: "💬", discord: "🎮" }[type] ?? "●";
}

function typeIconClass(type: string): string {
	return (
		{
			server:
				"bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-400",
			signal:
				"bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-400",
			slack:
				"bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-400",
			discord:
				"bg-indigo-100 text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-400",
		}[type] ?? "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400"
	);
}

function typeBadgeClass(type: string): string {
	return (
		{
			server:
				"bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-400",
			signal:
				"bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-400",
			slack:
				"bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-400",
			discord:
				"bg-indigo-100 text-indigo-700 dark:bg-indigo-900/40 dark:text-indigo-400",
		}[type] ?? "bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400"
	);
}

function fmtDate(ts: string): string {
	if (!ts) return "—";
	const d = new Date(ts);
	return (
		d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) +
		" " +
		d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
	);
}

function fmtBytes(bytes: number): string {
	if (bytes >= 1024 * 1024 * 1024) {
		return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
	}
	if (bytes >= 1024 * 1024) {
		return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
	}
	return `${(bytes / 1024).toFixed(0)} kB`;
}

function memPct(bytes: number): string {
	return `${Math.min(100, Math.round((bytes / maxMem.value) * 100))}%`;
}

function memBarClass(bytes: number): string {
	const pct = (bytes / maxMem.value) * 100;
	if (pct > 75) {
		return "bg-red-500";
	}
	if (pct > 40) {
		return "bg-yellow-400";
	}
	return "bg-blue-500";
}

function statusDotClass(status: string): string {
	if (status === "running" || status === "sleeping" || status === "connected")
		return "bg-green-500";
	if (status === "disk-wait") return "bg-yellow-400 animate-pulse";
	if (
		status === "gone" ||
		status === "zombie" ||
		status === "stopped" ||
		status === "unreachable" ||
		status === "error"
	)
		return "bg-red-500";
	return "bg-green-500";
}

function statusTextClass(status: string): string {
	if (status === "running" || status === "sleeping" || status === "connected")
		return "text-gray-500 dark:text-gray-400";
	if (
		status === "gone" ||
		status === "zombie" ||
		status === "stopped" ||
		status === "unreachable" ||
		status === "error"
	)
		return "text-red-500 dark:text-red-400";
	return "text-yellow-600 dark:text-yellow-400";
}

function capitalize(s: string): string {
	if (!s) return "Running";
	return s.charAt(0).toUpperCase() + s.slice(1);
}

function statusClass(status: string): string {
	if (status === "running" || status === "sleeping" || status === "connected") {
		return "text-green-600 dark:text-green-400";
	}
	if (
		status === "gone" ||
		status === "zombie" ||
		status === "unreachable" ||
		status === "error"
	) {
		return "text-red-500 dark:text-red-400";
	}
	return "text-gray-600 dark:text-gray-400";
}

function timeSince(d: Date): string {
	const s = Math.round((Date.now() - d.getTime()) / 1000);
	if (s < 5) return "just now";
	if (s < 60) return `${s}s ago`;
	return `${Math.round(s / 60)}m ago`;
}

onMounted(() => {
	fetchDaemons();
	refreshTimer = setInterval(fetchDaemons, 5000);
});

onUnmounted(() => {
	if (refreshTimer) clearInterval(refreshTimer);
	for (const key of Object.keys(logSources)) {
		closeLogs(key);
	}
});
</script>
