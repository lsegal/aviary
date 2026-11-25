import { computed, ref } from "vue";
import {
	type AppNotification,
	defaultNotificationPrefs,
	isViewing,
	type NotificationPrefs,
	normalizeNotificationPrefs,
	type SystemPermission,
	systemPermission,
} from "../lib/notifications";
import { playSound } from "../lib/notificationSounds";
import router from "../router";
import { useAuthStore } from "../stores/auth";
import { useToasts } from "./useToasts";

// The web UI's side of notifications (#1215): the bell's history, the big
// banners, delivery of each pushed notification, and the user's
// notification settings, kept on the server like the display preferences
// so they follow the user across devices, with a local copy for when the
// server can't be reached.

const PREFS_ENDPOINT = "/api/preferences/notifications";
const PREFS_STORAGE_KEY = "aviary.notifications.prefs";
const BANNER_MS = 12_000;

function storedPrefs(): NotificationPrefs {
	try {
		return normalizeNotificationPrefs(
			JSON.parse(localStorage.getItem(PREFS_STORAGE_KEY) ?? "null"),
		);
	} catch {
		return defaultNotificationPrefs();
	}
}

const prefs = ref<NotificationPrefs>(storedPrefs());
const history = ref<AppNotification[]>([]);
const unread = ref(0);
/** Big-style notifications showing across the top. */
const banners = ref<AppNotification[]>([]);
const permission = ref<SystemPermission>(systemPermission());
let prefsLoaded = false;

async function request<T>(
	method: string,
	path: string,
	body?: unknown,
): Promise<T | undefined> {
	const token = useAuthStore().getToken();
	try {
		const res = await fetch(path, {
			method,
			headers: {
				...(token ? { Authorization: `Bearer ${token}` } : {}),
				...(body !== undefined ? { "Content-Type": "application/json" } : {}),
			},
			body: body !== undefined ? JSON.stringify(body) : undefined,
		});
		if (!res.ok) return undefined;
		return (await res.json()) as T;
	} catch {
		return undefined;
	}
}

function showPrefs(next: NotificationPrefs) {
	prefs.value = next;
	try {
		localStorage.setItem(PREFS_STORAGE_KEY, JSON.stringify(next));
	} catch {
		// Private browsing can refuse storage; the server copy still applies.
	}
}

interface HistoryReply {
	notifications: AppNotification[];
	unread: number;
}

function showHistory(reply: HistoryReply | undefined) {
	if (!reply) return;
	history.value = reply.notifications ?? [];
	unread.value = reply.unread ?? 0;
}

/** Reloads the bell's history from the server. */
async function refresh() {
	showHistory(await request<HistoryReply>("GET", "/api/notifications"));
}

async function markRead(ids: string[] = []) {
	const set = new Set(ids);
	for (const n of history.value) if (!ids.length || set.has(n.id)) n.read = true;
	unread.value = history.value.filter((n) => !n.read).length;
	showHistory(
		await request<HistoryReply>("POST", "/api/notifications/read", { ids }),
	);
}

async function clear() {
	history.value = [];
	unread.value = 0;
	banners.value = [];
	showHistory(await request<HistoryReply>("DELETE", "/api/notifications"));
}

/** Opens n's linked page and marks it read. */
function open(n: AppNotification) {
	dismissBanner(n.id);
	if (!n.read) void markRead([n.id]);
	if (n.link) void router.push(n.link);
}

function dismissBanner(id: string) {
	banners.value = banners.value.filter((b) => b.id !== id);
}

function showBanner(n: AppNotification) {
	banners.value = [...banners.value.filter((b) => b.id !== n.id), n].slice(-3);
	setTimeout(() => dismissBanner(n.id), BANNER_MS);
}

function showToast(n: AppNotification) {
	useToasts().show(n.body ? `${n.title}: ${n.body}` : n.title, n.severity === "error" ? "error" : "info", {
		title: n.title,
		body: n.body,
		onClick: n.link ? () => open(n) : undefined,
		testId: "notification-toast",
	});
}

/** Shows n through the OS; false when the browser can't. */
function showSystem(n: AppNotification): boolean {
	permission.value = systemPermission();
	if (!prefs.value.system || permission.value !== "granted") return false;
	const options: NotificationOptions = {
		body: n.body,
		tag: n.id,
		icon: "/icons/icon-192.png",
		data: { link: n.link ?? "" },
	};
	const title = n.title || "Aviary";
	// An installed app shows it through its service worker, whose click
	// handler opens the link; a plain tab shows it itself.
	const viaWorker = navigator.serviceWorker?.controller
		? navigator.serviceWorker.ready.then((reg) =>
				reg.showNotification(title, options),
			)
		: null;
	if (viaWorker) {
		viaWorker.catch(() => undefined);
		return true;
	}
	try {
		const shown = new Notification(title, options);
		shown.onclick = () => {
			window.focus();
			open(n);
			shown.close();
		};
		return true;
	} catch {
		return false;
	}
}

/**
 * Delivers a notification the server pushed: into the bell, then as its
 * event type's style, with its sound, unless the user is looking at its
 * page already.
 */
function receive(n: AppNotification) {
	history.value = [n, ...history.value.filter((h) => h.id !== n.id)].slice(
		0,
		100,
	);
	if (!n.read) unread.value = history.value.filter((h) => !h.read).length;
	const visible = document.visibilityState === "visible";
	if (isViewing(n.link, router.currentRoute.value.path, visible)) {
		void markRead([n.id]);
		return;
	}
	if (n.quiet || !prefs.value.enabled) return;
	// A hidden tab can only reach the user through the OS.
	const wantSystem = n.delivery === "system" || !visible;
	const shownBySystem = wantSystem && showSystem(n);
	if (!shownBySystem && prefs.value.inApp) {
		if (n.delivery === "big") showBanner(n);
		else showToast(n);
	}
	if (n.sound) playSound(n.sound, prefs.value.sound.volume);
}

export function useNotifications() {
	const auth = useAuthStore();
	if (!prefsLoaded && auth.isLoggedIn) {
		prefsLoaded = true;
		void request<unknown>("GET", PREFS_ENDPOINT).then((remote) => {
			if (remote) showPrefs(normalizeNotificationPrefs(remote));
		});
	}
	return {
		prefs,
		history,
		unread,
		banners,
		permission,
		hasUnread: computed(() => unread.value > 0),
		refresh,
		receive,
		markRead,
		clear,
		open,
		dismissBanner,
		/** Changes some settings, here and on the server. */
		set(change: Partial<NotificationPrefs>) {
			const next = { ...prefs.value, ...change };
			showPrefs(next);
			void request("PUT", PREFS_ENDPOINT, next);
		},
		/** Restores the default settings, here and on the server. */
		reset() {
			showPrefs(defaultNotificationPrefs());
			void request("DELETE", PREFS_ENDPOINT);
		},
		/** Asks the browser for permission to show system notifications. */
		async requestPermission() {
			permission.value = systemPermission();
			if (permission.value !== "default") return permission.value;
			try {
				await Notification.requestPermission();
			} catch {
				// Old Safari takes a callback instead; the state is read below.
			}
			permission.value = systemPermission();
			return permission.value;
		},
		/** Sends a test notification through every enabled method. */
		test() {
			return request("POST", "/api/notifications/test");
		},
	};
}
