import { onMounted, onUnmounted, ref } from "vue";
import { useAuthStore } from "../stores/auth";

export type ConnectionStatus = "connecting" | "connected" | "disconnected";

/**
 * Maintains a WebSocket connection to /api/ws and exposes reactive server
 * status.  Automatically reconnects with exponential back-off (1 s → 30 s)
 * whenever the connection is lost.
 *
 * Intended to be called once from the top-level layout component.
 */
export function useServerStatus() {
	const auth = useAuthStore();

	const status = ref<ConnectionStatus>("connecting");
	const version = ref<string>("");
	const goos = ref<string>("");

	let ws: WebSocket | null = null;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let reconnectDelay = 1_000;
	let destroyed = false;

	function connect() {
		if (destroyed) return;

		status.value = "connecting";

		const protocol = location.protocol === "https:" ? "wss:" : "ws:";
		// Pass the token as a query param — browsers can't set Authorization
		// headers on WebSocket connections.  The session cookie is also
		// accepted by the server if present.
		const tok = auth.getToken();
		const qs = tok ? `?token=${encodeURIComponent(tok)}` : "";
		const url = `${protocol}//${location.host}/api/ws${qs}`;

		ws = new WebSocket(url);

		ws.onopen = () => {
			reconnectDelay = 1_000; // reset back-off on success
		};

		ws.onmessage = (e: MessageEvent) => {
			try {
				const data = JSON.parse(e.data as string) as {
					ok?: boolean;
					version?: string;
					goos?: string;
				};
				if (data.version) version.value = data.version;
				if (data.goos) goos.value = data.goos;
				status.value = "connected";
			} catch {
				// ignore malformed frames
			}
		};

		ws.onclose = () => {
			ws = null;
			if (destroyed) return;
			status.value = "disconnected";
			scheduleReconnect();
		};

		// onerror always precedes onclose; just log — onclose handles reconnect.
		ws.onerror = () => {
			/* intentionally empty */
		};
	}

	function scheduleReconnect() {
		if (destroyed) return;
		reconnectTimer = setTimeout(() => {
			connect();
		}, reconnectDelay);
		reconnectDelay = Math.min(reconnectDelay * 2, 30_000);
	}

	function teardown() {
		destroyed = true;
		if (reconnectTimer !== null) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
		if (ws) {
			ws.onclose = null; // prevent reconnect loop
			ws.close();
			ws = null;
		}
	}

	onMounted(() => connect());
	onUnmounted(() => teardown());

	return { status, version, goos };
}
