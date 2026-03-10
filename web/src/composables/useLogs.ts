import { computed, onUnmounted, ref } from "vue";
import { useAuthStore } from "../stores/auth";

export interface LogEntry {
	seq: number;
	ts: string;
	level: "debug" | "info" | "warn" | "error";
	component: string;
	msg: string;
	attrs?: Record<string, string>;
}

export type LogLevel = "debug" | "info" | "warn" | "error";

const LEVEL_ORDER: Record<LogLevel, number> = {
	debug: 0,
	info: 1,
	warn: 2,
	error: 3,
};

const INITIAL_TAIL = 200;
const HISTORY_PAGE = 200;
const MAX_ENTRIES = 2000;
const TRIM_TO = 1800;

export function useLogs() {
	const auth = useAuthStore();

	const entries = ref<LogEntry[]>([]);
	const connected = ref(false);
	const error = ref<string | null>(null);
	const hasMore = ref(false);
	const loadingMore = ref(false);
	// How many lines from the end of the file we've already loaded via SSE tail.
	let historySkip = INITIAL_TAIL;

	// Filter state.
	const filterComponents = ref<Set<string>>(new Set());
	const filterLevel = ref<LogLevel>("debug");
	const filterText = ref("");

	// All unique components seen so far.
	const allComponents = computed(() => {
		const seen = new Set<string>();
		for (const e of entries.value) seen.add(e.component);
		return Array.from(seen).sort();
	});

	// Filtered view.
	const filtered = computed(() => {
		const minLevel = LEVEL_ORDER[filterLevel.value];
		const text = filterText.value.trim().toLowerCase();
		return entries.value.filter((e) => {
			if (LEVEL_ORDER[e.level] < minLevel) return false;
			if (
				filterComponents.value.size > 0 &&
				!filterComponents.value.has(e.component)
			)
				return false;
			if (text) {
				const haystack = `${e.component} ${e.msg} ${Object.entries(
					e.attrs ?? {},
				)
					.map(([k, v]) => `${k}=${v}`)
					.join(" ")}`.toLowerCase();
				if (!haystack.includes(text)) return false;
			}
			return true;
		});
	});

	let es: EventSource | null = null;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectDelay = 1_000;
	let destroyed = false;
	let pendingEntries: LogEntry[] = [];
	let flushTimer: ReturnType<typeof setTimeout> | null = null;

	function scheduleFlush() {
		if (flushTimer !== null) return;
		flushTimer = setTimeout(() => {
			flushTimer = null;
			if (pendingEntries.length === 0) return;
			const next = entries.value.concat(pendingEntries);
			pendingEntries = [];
			entries.value = next.length > MAX_ENTRIES ? next.slice(-TRIM_TO) : next;
		}, 32);
	}

	function connect() {
		if (destroyed) return;
		const tok = auth.getToken();
		const qs = new URLSearchParams({ tail: String(INITIAL_TAIL) });
		if (tok) qs.set("token", tok);
		es = new EventSource(`/api/logs?${qs.toString()}`);

		es.onopen = () => {
			connected.value = true;
			error.value = null;
			reconnectDelay = 1_000;
			// Reset skip counter; the SSE tail re-sends the last INITIAL_TAIL lines.
			historySkip = INITIAL_TAIL;
			// Optimistically assume there may be history before the tail.
			hasMore.value = true;
		};

		es.onmessage = (evt: MessageEvent) => {
			try {
				const entry = JSON.parse(evt.data as string) as LogEntry;
				pendingEntries.push(entry);
				scheduleFlush();
			} catch {
				// ignore malformed frames
			}
		};

		es.onerror = () => {
			es?.close();
			es = null;
			connected.value = false;
			if (destroyed) return;
			error.value = "Log stream disconnected — reconnecting…";
			scheduleReconnect();
		};
	}

	function scheduleReconnect() {
		reconnectTimer = setTimeout(() => {
			reconnectDelay = Math.min(reconnectDelay * 2, 30_000);
			connect();
		}, reconnectDelay);
	}

	function disconnect() {
		destroyed = true;
		pendingEntries = [];
		if (flushTimer !== null) {
			clearTimeout(flushTimer);
			flushTimer = null;
		}
		if (reconnectTimer !== null) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
		es?.close();
		es = null;
	}

	async function loadPrevious() {
		if (loadingMore.value) return;
		loadingMore.value = true;
		try {
			const tok = auth.getToken();
			const qs = new URLSearchParams({
				skip: String(historySkip),
				limit: String(HISTORY_PAGE),
			});
			if (tok) qs.set("token", tok);
			const res = await fetch(`/api/logs/history?${qs}`);
			if (!res.ok) return;
			const data = (await res.json()) as {
				entries: LogEntry[];
				hasMore: boolean;
			};
			if (data.entries && data.entries.length > 0) {
				historySkip += data.entries.length;
				// Prepend with negative seq values to keep them before live entries.
				const offset =
					entries.value.length > 0
						? entries.value[0].seq - data.entries.length - 1
						: -data.entries.length;
				const prepend = data.entries.map((e, i) => ({ ...e, seq: offset + i }));
				const next = [...prepend, ...entries.value];
				entries.value = next.length > MAX_ENTRIES ? next.slice(-TRIM_TO) : next;
			}
			hasMore.value = data.hasMore;
		} catch {
			// ignore
		} finally {
			loadingMore.value = false;
		}
	}

	function clearLogs() {
		entries.value = [];
		historySkip = INITIAL_TAIL;
		hasMore.value = true;
	}

	function toggleComponent(component: string) {
		if (filterComponents.value.has(component)) {
			filterComponents.value.delete(component);
		} else {
			filterComponents.value.add(component);
		}
		// Trigger reactivity by replacing the set reference.
		filterComponents.value = new Set(filterComponents.value);
	}

	connect();
	onUnmounted(disconnect);

	return {
		entries,
		filtered,
		allComponents,
		connected,
		error,
		hasMore,
		loadingMore,
		filterComponents,
		filterLevel,
		filterText,
		historyPageSize: HISTORY_PAGE,
		toggleComponent,
		loadPrevious,
		clearLogs,
	};
}
