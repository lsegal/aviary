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

export function useLogs() {
	const auth = useAuthStore();

	const entries = ref<LogEntry[]>([]);
	const connected = ref(false);
	const error = ref<string | null>(null);

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

	function connect() {
		if (destroyed) return;
		const tok = auth.getToken();
		const qs = tok ? `?token=${encodeURIComponent(tok)}` : "";
		es = new EventSource(`/api/logs${qs}`);

		es.onopen = () => {
			connected.value = true;
			error.value = null;
			reconnectDelay = 1_000;
		};

		es.onmessage = (evt: MessageEvent) => {
			try {
				const entry = JSON.parse(evt.data as string) as LogEntry;
				// Keep the ring at ≤ 2000 entries in the UI.
				if (entries.value.length >= 2000) {
					entries.value = entries.value.slice(-1800);
				}
				entries.value.push(entry);
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
		if (reconnectTimer !== null) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
		es?.close();
		es = null;
	}

	function clearLogs() {
		entries.value = [];
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
		filterComponents,
		filterLevel,
		filterText,
		toggleComponent,
		clearLogs,
	};
}
