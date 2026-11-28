// The fake server side of notifications (#1215) for the notifications
// component tests (#1636): MCP, a notification history the bell reads and
// changes, the preferences endpoint, and the live channel a test pushes on.
// Shared by notifications*.component.test.ts, which are split by how much
// fresh module state (useNotifications loads preferences once) they need.

import { createPinia, setActivePinia } from "pinia";
import { expect } from "vitest";
import { page } from "vitest/browser";
import {
	mockMCP,
	route,
	routeWebSocket,
	type ToolFixtures,
	type WebSocketRoute,
} from "../../component-test/server";
import { useNotifications } from "../../composables/useNotifications";
import { useToasts } from "../../composables/useToasts";

const health = JSON.stringify({ type: "health", ok: true, version: "v1" });

export interface Note {
	id: string;
	type: string;
	severity: string;
	title: string;
	body?: string;
	link?: string;
	section: string;
	time: string;
	read: boolean;
	count: number;
	delivery: "big" | "small" | "system";
	sound?: string;
	quiet?: boolean;
	pauseTask?: string;
}

let seq = 0;
export function note(over: Partial<Note> = {}): Note {
	seq += 1;
	return {
		id: `n${seq}`,
		type: "codeloop.failed",
		severity: "error",
		title: `Failed: acme/app#${seq}`,
		body: "agent exited with status 1",
		link: "/system/usage",
		section: "codeloop",
		time: new Date().toISOString(),
		read: false,
		count: 1,
		delivery: "small",
		...over,
	};
}

/**
 * Mocks the server: MCP, a notification history the bell reads and
 * changes, and the live channel, returned so a test can push.
 */
export function setup(
	options: {
		history?: Note[];
		prefs?: Record<string, unknown>;
		tools?: ToolFixtures;
	} = {},
) {
	// Toasts and banners are app-wide module state; a page load starts
	// without any, so clear what an earlier test in the file left.
	// (useNotifications needs a store before the app mounts its own.)
	setActivePinia(createPinia());
	const { toasts } = useToasts();
	toasts.value = [];
	useNotifications().banners.value = [];

	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: [],
		auth_list: [],
		usage_query: [],
		config_get: {
			agents: [{ name: "assistant", model: "x/y", fallbacks: [] }],
		},
		...options.tools,
	});
	const state = {
		history: [...(options.history ?? [])],
		prefs: options.prefs ?? {},
		puts: [] as Record<string, unknown>[],
		reads: [] as string[][],
		tests: 0,
	};
	const reply = () => ({
		notifications: state.history,
		unread: state.history.filter((n) => !n.read).length,
	});
	route(/\/api\/notifications(\/.*)?$/, async (r) => {
		const req = r.request();
		const path = new URL(req.url()).pathname;
		if (path.endsWith("/read")) {
			const ids = (req.postDataJSON()?.ids as string[]) ?? [];
			state.reads.push(ids);
			for (const n of state.history)
				if (!ids.length || ids.includes(n.id)) n.read = true;
		} else if (path.endsWith("/test")) {
			state.tests += 1;
		} else if (req.method() === "DELETE") {
			state.history = [];
		}
		await r.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(reply()),
		});
	});
	route("/api/preferences/notifications", async (r) => {
		const req = r.request();
		if (req.method() === "PUT") {
			state.prefs = req.postDataJSON();
			state.puts.push(state.prefs);
		}
		if (req.method() === "DELETE") state.prefs = {};
		await r.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(state.prefs),
		});
	});
	// Every connection gets each push, as the server broadcasts; the app
	// keeps only one open.
	const sockets: WebSocketRoute[] = [];
	routeWebSocket(/\/api\/ws/, (ws) => {
		sockets.push(ws);
		ws.send(health);
	});
	return {
		state,
		/** Pushes n as the server does, adding it to the history. */
		async push(n: Note) {
			await expect
				.poll(
					() =>
						page
							.locator(
								'[data-testid="connection-status"][data-state="connected"]',
							)
							.elements().length,
				)
				.toBeGreaterThan(0);
			state.history.unshift(n);
			for (const ws of sockets)
				ws.send(JSON.stringify({ type: "notification", notification: n }));
		},
	};
}

const realNotification = Object.getOwnPropertyDescriptor(
	window,
	"Notification",
);

/** Replaces the browser's Notification API with one that records; returns
 * what it showed. */
export function stubSystemNotifications(permission: string) {
	const shown: { title: string; body?: string }[] = [];
	class FakeNotification {
		static permission = permission;
		static requestPermission() {
			return Promise.resolve(permission);
		}
		onclick: (() => void) | null = null;
		constructor(title: string, options?: { body?: string }) {
			shown.push({ title, body: options?.body });
		}
		close() {}
	}
	Object.defineProperty(window, "Notification", {
		configurable: true,
		writable: true,
		value: FakeNotification,
	});
	return shown;
}

/** Removes the browser's Notification API, as a browser without it. */
export function removeSystemNotifications() {
	delete (window as unknown as { Notification?: unknown }).Notification;
}

/** Puts the browser's own Notification API back. */
export function restoreSystemNotifications() {
	if (realNotification)
		Object.defineProperty(window, "Notification", realNotification);
}

/** Replaces Web Audio with a recorder of each sound's tones and volume;
 * returns what it played. */
export function stubAudio() {
	const played: { gain: number; freqs: number[] }[] = [];
	const param = () => ({
		value: 0,
		setValueAtTime() {},
		exponentialRampToValueAtTime() {},
	});
	class FakeAudioContext {
		state = "running";
		currentTime = 0;
		destination = {};
		resume() {
			return Promise.resolve();
		}
		createGain() {
			const node = { gain: param(), connect: (n: unknown) => n };
			if (!played.length || played.at(-1)?.freqs.length) {
				// The first gain of a sound is its master volume.
				const sound = { gain: 0, freqs: [] as number[] };
				played.push(sound);
				Object.defineProperty(node.gain, "value", {
					set: (v: number) => {
						sound.gain = v;
					},
					get: () => sound.gain,
				});
			}
			return node;
		}
		createOscillator() {
			const sound = played.at(-1);
			const osc = {
				type: "sine",
				frequency: { value: 0 },
				connect: (n: unknown) => n,
				start() {},
				stop() {},
			};
			Object.defineProperty(osc.frequency, "value", {
				set: (v: number) => sound?.freqs.push(v),
				get: () => 0,
			});
			return osc;
		}
	}
	(window as unknown as { AudioContext: unknown }).AudioContext =
		FakeAudioContext;
	return played;
}
