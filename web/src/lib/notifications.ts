// Notifications (#1215), mirroring internal/notify: the server emits,
// de-duplicates and rate-limits them and pushes each over /api/ws; the web
// UI shows them as a banner, a toast or a system notification, plays their
// sound, and keeps the bell's history.

export type Delivery = "big" | "small" | "system";
export type Severity = "info" | "success" | "warning" | "error";

/** A notification as the server sends it. */
export interface AppNotification {
	id: string;
	type: string;
	severity: Severity;
	title: string;
	body?: string;
	/** The web UI path it opens: a chat, job card, run or task. */
	link?: string;
	section: string;
	time: string;
	read: boolean;
	/** More than one for repeats or a grouped burst. */
	count: number;
	delivery: Delivery;
	/** The sound to play, empty for none. */
	sound?: string;
	/** Arrived during quiet hours: kept in the bell, not shown or played. */
	quiet?: boolean;
	/**
	 * The "<agent>/<task>" its Pause action pauses, on a notification that a
	 * task has failed many times in a row (#1419).
	 */
	pauseTask?: string;
}

export interface NotificationEventPrefs {
	enabled: boolean;
	delivery: Delivery;
	/** "" for the default sound, "none", or a sound. */
	sound?: string;
}

/** Mirrors store.NotificationRepeats: how repeating failures notify. */
export interface NotificationRepeats {
	/** Merge repeats of the same failure into one entry, delivered once. */
	suppress: boolean;
	/** How long after the last repeat the next one still merges. */
	windowMinutes: number;
	/** Notify once when a source fails streakCount times in a row. */
	streak: boolean;
	streakCount: number;
}

/** Mirrors store.NotificationPreferences. */
export interface NotificationPrefs {
	enabled: boolean;
	system: boolean;
	inApp: boolean;
	server: boolean;
	sound: { enabled: boolean; type: string; volume: number };
	quietHours: { enabled: boolean; start: string; end: string };
	repeats: NotificationRepeats;
	events: Record<string, NotificationEventPrefs>;
}

/** The repeat windows Settings offers, as notify.MaxRepeatWindowMinutes caps. */
export const REPEAT_WINDOWS = [
	{ value: 15, label: "15 minutes" },
	{ value: 60, label: "1 hour" },
	{ value: 360, label: "6 hours" },
	{ value: 1440, label: "1 day" },
	{ value: 10080, label: "1 week" },
];

/** The in-a-row counts Settings offers. */
export const STREAK_COUNTS = [3, 5, 10, 20].map((n) => ({
	value: n,
	label: `${n} failures`,
}));

export const SOUNDS = [
	{ value: "chime", label: "Chime" },
	{ value: "bell", label: "Bell" },
	{ value: "ping", label: "Ping" },
	{ value: "pop", label: "Pop" },
	{ value: "marimba", label: "Marimba" },
	{ value: "alert", label: "Alert" },
];

export const DELIVERIES: { value: Delivery; label: string }[] = [
	{ value: "big", label: "Banner" },
	{ value: "small", label: "Toast" },
	{ value: "system", label: "System" },
];

export interface EventTypeInfo {
	type: string;
	section: string;
	label: string;
	default: NotificationEventPrefs;
}

/** The configurable event types, in settings order, as notify.EventTypes. */
export const EVENT_TYPES: EventTypeInfo[] = [
	{
		type: "chat.reply",
		section: "Chats",
		label: "Agent replied in a chat you're not viewing",
		default: { enabled: true, delivery: "small" },
	},
	{
		type: "codeloop.failed",
		section: "Code Loop",
		label: "Job failed",
		default: { enabled: true, delivery: "big" },
	},
	{
		type: "codeloop.blocked",
		section: "Code Loop",
		label: "Job blocked or waiting on you",
		default: { enabled: true, delivery: "big" },
	},
	{
		type: "codeloop.done",
		section: "Code Loop",
		label: "Job done or pull request merged",
		default: { enabled: false, delivery: "small" },
	},
	{
		type: "task.run",
		section: "Scheduled tasks & workflows",
		label: "Run finished (Send via: Notify)",
		default: { enabled: true, delivery: "small" },
	},
	{
		type: "workflow.failed",
		section: "Scheduled tasks & workflows",
		label: "Workflow failed",
		default: { enabled: true, delivery: "small" },
	},
	{
		type: "custom",
		section: "Agents & API",
		label: "Sent by an agent or the notify API",
		default: { enabled: true, delivery: "small" },
	},
];

export function defaultNotificationPrefs(): NotificationPrefs {
	return {
		enabled: true,
		system: true,
		inApp: true,
		server: false,
		sound: { enabled: false, type: "chime", volume: 60 },
		quietHours: { enabled: false, start: "22:00", end: "07:00" },
		repeats: {
			suppress: true,
			windowMinutes: 360,
			streak: true,
			streakCount: 5,
		},
		events: Object.fromEntries(
			EVENT_TYPES.map((t) => [t.type, { ...t.default }]),
		),
	};
}

const CLOCK = /^([01]\d|2[0-3]):[0-5]\d$/;

/** prefs as the server sent them, with what's missing or unknown defaulted. */
export function normalizeNotificationPrefs(raw: unknown): NotificationPrefs {
	const prefs = defaultNotificationPrefs();
	if (!raw || typeof raw !== "object") return prefs;
	const r = raw as Partial<NotificationPrefs>;
	for (const key of ["enabled", "system", "inApp", "server"] as const) {
		if (typeof r[key] === "boolean") prefs[key] = r[key];
	}
	if (r.sound) {
		if (typeof r.sound.enabled === "boolean")
			prefs.sound.enabled = r.sound.enabled;
		if (SOUNDS.some((s) => s.value === r.sound?.type))
			prefs.sound.type = r.sound.type;
		if (typeof r.sound.volume === "number")
			prefs.sound.volume = Math.min(100, Math.max(0, r.sound.volume));
	}
	if (r.quietHours) {
		if (typeof r.quietHours.enabled === "boolean")
			prefs.quietHours.enabled = r.quietHours.enabled;
		if (CLOCK.test(r.quietHours.start ?? ""))
			prefs.quietHours.start = r.quietHours.start;
		if (CLOCK.test(r.quietHours.end ?? ""))
			prefs.quietHours.end = r.quietHours.end;
	}
	if (r.repeats) {
		const rep = r.repeats;
		if (typeof rep.suppress === "boolean")
			prefs.repeats.suppress = rep.suppress;
		if (typeof rep.streak === "boolean") prefs.repeats.streak = rep.streak;
		if (typeof rep.windowMinutes === "number")
			prefs.repeats.windowMinutes = Math.min(
				10080,
				Math.max(1, Math.round(rep.windowMinutes)),
			);
		if (typeof rep.streakCount === "number")
			prefs.repeats.streakCount = Math.min(
				100,
				Math.max(2, Math.round(rep.streakCount)),
			);
	}
	for (const t of EVENT_TYPES) {
		const ev = r.events?.[t.type];
		if (!ev) continue;
		prefs.events[t.type] = {
			enabled: typeof ev.enabled === "boolean" ? ev.enabled : t.default.enabled,
			delivery: DELIVERIES.some((d) => d.value === ev.delivery)
				? ev.delivery
				: t.default.delivery,
			sound:
				ev.sound === "none" || SOUNDS.some((s) => s.value === ev.sound)
					? ev.sound
					: "",
		};
	}
	return prefs;
}

/** Why the browser can or can't show system notifications. */
export type SystemPermission =
	| "granted"
	| "denied"
	| "default"
	| "unsupported"
	| "insecure";

export function systemPermission(): SystemPermission {
	if (!window.isSecureContext) return "insecure";
	if (typeof Notification === "undefined") return "unsupported";
	return Notification.permission;
}

/**
 * Whether the user is looking at link right now: it is the current page and
 * the tab is visible. Notifications for it are not shown.
 */
export function isViewing(
	link: string | undefined,
	currentPath: string,
	visible: boolean,
): boolean {
	if (!link || !visible) return false;
	const norm = (p: string) => {
		try {
			return decodeURIComponent(p).replace(/\/+$/, "");
		} catch {
			return p.replace(/\/+$/, "");
		}
	};
	return norm(link.split(/[?#]/)[0]) === norm(currentPath);
}

/** A notification's age, such as "2m" or "3h", for the bell. */
export function ago(time: string, now = Date.now()): string {
	const secs = Math.max(0, Math.round((now - Date.parse(time)) / 1000));
	if (secs < 60) return "now";
	if (secs < 3600) return `${Math.floor(secs / 60)}m`;
	if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
	return `${Math.floor(secs / 86400)}d`;
}
