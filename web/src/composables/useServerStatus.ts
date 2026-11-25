import { onMounted, onUnmounted, ref } from "vue";
import type { AppNotification } from "../lib/notifications";
import { useAuthStore } from "../stores/auth";
import { useSettingsStore } from "../stores/settings";
import { applyProjectEvent, type ProjectEvent } from "./projectListStore";
import { useMCP } from "./useMCP";
import { useNotifications } from "./useNotifications";

export type ConnectionStatus = "connecting" | "connected" | "disconnected";

/** A running job that a restart or upgrade stops and resumes afterwards. */
export interface RestartJob {
	source: string;
	id: string;
	title: string;
}

/** Mirrors saferestart.Status on the server. */
export interface RestartStatus {
	phase: "idle" | "waiting" | "draining" | "updating";
	when?: "now" | "idle";
	upgrade?: boolean;
	version?: string;
	force?: boolean;
	/** In-flight MCP calls a draining restart waits on. */
	mcpCalls?: number;
	jobs: RestartJob[];
	held: boolean;
	noop?: boolean;
	emulated?: boolean;
	message?: string;
	error?: string;
	autoUpdate?: AutoUpdateStatus;
}

/** Mirrors saferestart.AutoUpdate: an automatic update not applied yet. */
export interface AutoUpdateStatus {
	version?: string;
	state?: "waiting-restart" | "applying";
	error?: string;
	/** Classifies error: another git command held the checkout's lock, or
	 * a stale .git/index.lock blocks the pull. */
	code?: "git-busy" | "git-stale-lock";
}

export type RestartWhen = "now" | "idle";

/** Mirrors update.GitState: set when a dev build updates its source checkout. */
export interface GitState {
	dir: string;
	branch: string;
	upstream?: string;
	behind: number;
	ahead: number;
	dirty: boolean;
	supervised: boolean;
	hotReload: boolean;
}

interface VersionCheck {
	latestVersion?: string;
	upgradeAvailable?: boolean;
	upgradeSupported?: boolean;
	git?: GitState;
	noCheckout?: boolean;
	message?: string;
	releaseUrl?: string;
	restart?: RestartStatus;
}

/**
 * Maintains a WebSocket connection to /api/ws and exposes reactive server
 * status.  Automatically reconnects with exponential back-off (1 s → 30 s)
 * whenever the connection is lost.
 *
 * Intended to be called once from the top-level layout component.
 */
export function useServerStatus() {
	const auth = useAuthStore();
	const settings = useSettingsStore();
	const { callTool } = useMCP();
	const notifications = useNotifications();

	const status = ref<ConnectionStatus>("connecting");
	const version = ref<string>("");
	const goos = ref<string>("");
	const latestVersion = ref<string>("");
	const upgradeAvailable = ref(false);
	// False when an update exists but cannot be applied, e.g. a dev checkout
	// with local changes; versionMessage says why.
	const upgradeSupported = ref(true);
	const git = ref<GitState | null>(null);
	// True for a dev build that cannot find its source checkout, so it cannot
	// tell whether an update exists; versionMessage says how to fix it.
	const noCheckout = ref(false);
	// Result of a git-mode update, shown once the update banner is gone.
	const updateNotice = ref("");
	const versionMessage = ref("");
	const checkingVersion = ref(false);
	const upgrading = ref(false);
	const releaseUrl = ref("");
	const restart = ref<RestartStatus | null>(null);
	const upgradeDialogOpen = ref(false);

	let ws: WebSocket | null = null;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let versionTimer: ReturnType<typeof setInterval> | null = null;
	let reconnectDelay = 1_000;
	let destroyed = false;
	let consecutiveFailures = 0;
	const maxConnectingFailures = 3;

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
					type?: string;
					ok?: boolean;
					version?: string;
					goos?: string;
					restart?: RestartStatus;
					check?: VersionCheck;
					notification?: AppNotification;
				};
				if (data.type === "notification" && data.notification) {
					notifications.receive(data.notification);
					return;
				}
				if (data.type === "notifications_changed") {
					void notifications.refresh();
					return;
				}
				if (data.type === "restart" && data.restart) {
					restart.value = data.restart;
					return;
				}
				if (data.type === "version" && data.check) {
					applyCheck(data.check);
					return;
				}
				// Projects added, removed or polled anywhere show here at once.
				if (applyProjectEvent(data as ProjectEvent)) return;
				// An agent, channel, task or workflow saved in another client.
				if (data.type === "config") {
					if (settings.config) void settings.fetchConfig();
					return;
				}
				if (data.type !== "health") return;
				const previousVersion = version.value;
				if (data.version) version.value = data.version;
				if (data.goos) goos.value = data.goos;
				if (data.restart) {
					restart.value = data.restart;
				} else if (restart.value && restart.value.phase !== "idle") {
					// The server came back without a pending restart: it finished.
					restart.value = null;
					if (previousVersion && previousVersion !== version.value) {
						void refreshVersionStatus();
					}
				}
				consecutiveFailures = 0;
				// Each (re)connection catches the bell up on what it missed.
				if (status.value !== "connected") void notifications.refresh();
				status.value = "connected";
			} catch {
				// ignore malformed frames
			}
		};

		ws.onclose = () => {
			ws = null;
			if (destroyed) return;
			consecutiveFailures += 1;
			status.value =
				consecutiveFailures > maxConnectingFailures
					? "disconnected"
					: "connecting";
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

	// Retries a lost connection now instead of waiting out the back-off.
	function reconnect() {
		if (destroyed || ws) return;
		if (reconnectTimer !== null) {
			clearTimeout(reconnectTimer);
			reconnectTimer = null;
		}
		reconnectDelay = 1_000;
		connect();
	}

	function applyCheck(data: VersionCheck) {
		latestVersion.value = data.latestVersion ?? "";
		upgradeAvailable.value = data.upgradeAvailable === true;
		upgradeSupported.value = data.upgradeSupported !== false;
		git.value = data.git ?? null;
		noCheckout.value = data.noCheckout === true;
		if (upgradeAvailable.value) updateNotice.value = "";
		versionMessage.value = data.message ?? "";
		releaseUrl.value = data.releaseUrl ?? "";
	}

	async function refreshVersionStatus() {
		checkingVersion.value = true;
		try {
			const data = JSON.parse(
				await callTool("server_version_check"),
			) as VersionCheck;
			applyCheck(data);
			if (data.restart) restart.value = data.restart;
		} catch {
			versionMessage.value = "";
		} finally {
			checkingVersion.value = false;
		}
	}

	/** Opens the upgrade dialog with a fresh list of the jobs it would stop. */
	async function openUpgradeDialog() {
		upgradeDialogOpen.value = true;
		await refreshVersionStatus();
	}

	async function triggerUpgrade(
		when: RestartWhen,
		opts: { removeStaleLock?: boolean } = {},
	) {
		upgrading.value = true;
		try {
			const st = JSON.parse(
				await callTool("server_upgrade", {
					version: latestVersion.value || undefined,
					when,
					remove_stale_lock: opts.removeStaleLock || undefined,
				}),
			) as RestartStatus;
			upgradeDialogOpen.value = false;
			if (st.noop) {
				versionMessage.value = st.message ?? "";
				if (!st.emulated) upgradeAvailable.value = false;
				if (git.value) updateNotice.value = st.message ?? "";
				return;
			}
			if (git.value && st.message) {
				// The pull is done; the restart onto it may still wait for jobs.
				upgradeAvailable.value = false;
				updateNotice.value = st.message;
			}
			restart.value = st;
		} catch (err) {
			versionMessage.value = err instanceof Error ? err.message : String(err);
		} finally {
			upgrading.value = false;
		}
	}

	/** Safely restarts the server without upgrading it. */
	async function triggerRestart() {
		try {
			restart.value = JSON.parse(
				await callTool("server_restart", {}),
			) as RestartStatus;
		} catch (err) {
			versionMessage.value = err instanceof Error ? err.message : String(err);
		}
	}

	async function cancelRestart() {
		try {
			restart.value = JSON.parse(
				await callTool("server_restart", { cancel: true }),
			) as RestartStatus;
		} catch (err) {
			versionMessage.value = err instanceof Error ? err.message : String(err);
		}
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
		if (versionTimer !== null) {
			clearInterval(versionTimer);
			versionTimer = null;
		}
	}

	onMounted(() => {
		connect();
		void refreshVersionStatus();
		versionTimer = setInterval(() => {
			void refreshVersionStatus();
		}, 5 * 60_000);
	});
	onUnmounted(() => teardown());

	return {
		status,
		version,
		goos,
		latestVersion,
		upgradeAvailable,
		upgradeSupported,
		git,
		noCheckout,
		updateNotice,
		versionMessage,
		checkingVersion,
		upgrading,
		releaseUrl,
		restart,
		upgradeDialogOpen,
		refreshVersionStatus,
		reconnect,
		openUpgradeDialog,
		triggerUpgrade,
		triggerRestart,
		cancelRestart,
	};
}
