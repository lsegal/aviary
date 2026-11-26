import { expect, type Page, test, type WebSocketRoute } from "@playwright/test";
import { mockMCP, setAuthToken, type ToolFixtures } from "./helpers/mockMCP";

// Notifications (#1215): pushed over /api/ws, shown as a banner, a toast or
// a system notification with a sound, kept in the bell, and controlled from
// Settings → Notifications.

const health = JSON.stringify({ type: "health", ok: true, version: "v1" });

interface Note {
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
function note(over: Partial<Note> = {}): Note {
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
async function setup(
	page: Page,
	options: {
		history?: Note[];
		prefs?: Record<string, unknown>;
		tools?: ToolFixtures;
	} = {},
) {
	await setAuthToken(page);
	await mockMCP(page, {
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
	await page.route(/\/api\/notifications(\/.*)?$/, async (route) => {
		const req = route.request();
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
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(reply()),
		});
	});
	await page.route("/api/preferences/notifications", async (route) => {
		const req = route.request();
		if (req.method() === "PUT") {
			state.prefs = req.postDataJSON();
			state.puts.push(state.prefs);
		}
		if (req.method() === "DELETE") state.prefs = {};
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(state.prefs),
		});
	});
	// Every connection gets each push, as the server broadcasts; the app
	// keeps only one open.
	const sockets: WebSocketRoute[] = [];
	await page.routeWebSocket(/\/api\/ws/, (ws) => {
		sockets.push(ws);
		ws.send(health);
	});
	return {
		state,
		/** Pushes n as the server does, adding it to the history. */
		async push(n: Note) {
			await expect(
				page
					.locator('[data-testid="connection-status"][data-state="connected"]')
					.first(),
			).toBeAttached();
			state.history.unshift(n);
			for (const ws of sockets)
				ws.send(JSON.stringify({ type: "notification", notification: n }));
		},
	};
}

/** Replaces the browser's Notification API with one that records. */
async function stubSystemNotifications(page: Page, permission: string) {
	await page.addInitScript((perm) => {
		const shown: { title: string; body?: string }[] = [];
		(window as unknown as { __shown: typeof shown }).__shown = shown;
		class FakeNotification {
			static permission = perm;
			static requestPermission() {
				return Promise.resolve(perm);
			}
			onclick: (() => void) | null = null;
			constructor(title: string, options?: { body?: string }) {
				shown.push({ title, body: options?.body });
			}
			close() {}
		}
		(window as unknown as { Notification: unknown }).Notification =
			FakeNotification;
	}, permission);
}

/** Replaces Web Audio with a recorder of each sound's tones and volume. */
async function stubAudio(page: Page) {
	await page.addInitScript(() => {
		const played: { gain: number; freqs: number[] }[] = [];
		(window as unknown as { __played: typeof played }).__played = played;
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
	});
}

test.use({ viewport: { width: 1280, height: 900 } });

test("Notifications is a Settings tab after Display, under the shared header, sound off by default", async ({
	page,
}) => {
	const { state } = await setup(page);
	await page.goto("/system/settings");
	const tabs = page.getByTestId("settings-tabs").getByRole("tab");
	await expect(tabs).toHaveText([
		"General",
		"Providers & Authentication",
		"Display",
		"Notifications",
	]);
	await tabs.last().click();
	await expect(page).toHaveURL("/system/notifications");
	await expect(page.getByTestId("settings-notifications")).toBeVisible();
	await expect(page.getByTestId("settings-title")).toHaveText("Settings");
	await expect(page.getByRole("button", { name: "Save Changes" })).toHaveCount(
		0,
	);

	await expect(page.getByTestId("notifications-enabled")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(page.getByTestId("notifications-sound")).toHaveAttribute(
		"aria-checked",
		"false",
	);

	// Each change saves on the server at once.
	await page.getByTestId("notifications-sound").click();
	await expect
		.poll(() => (state.puts.at(-1)?.sound as { enabled?: boolean })?.enabled)
		.toBe(true);
	await page
		.getByTestId("notification-event-chat.reply")
		.getByTestId("notification-event-delivery")
		.locator('[data-value="big"]')
		.click();
	await expect
		.poll(
			() =>
				(state.puts.at(-1)?.events as Record<string, { delivery: string }>)?.[
					"chat.reply"
				]?.delivery,
		)
		.toBe("big");

	// Saved settings load on another device.
	await page.evaluate(() => localStorage.clear());
	await page.reload();
	await expect(page.getByTestId("notifications-sound")).toHaveAttribute(
		"aria-checked",
		"true",
	);

	await page.getByTestId("notifications-test").click();
	await expect.poll(() => state.tests).toBe(1);
});

test("the Notifications tab fits a phone-width screen", async ({ page }) => {
	await page.setViewportSize({ width: 375, height: 812 });
	await setup(page);
	await page.goto("/system/notifications");
	await expect(page.getByTestId("settings-notifications")).toBeVisible();
	const overflow = await page
		.getByTestId("settings-scroll")
		.evaluate((el) => el.scrollWidth - el.clientWidth);
	expect(overflow).toBeLessThanOrEqual(0);
	await expect(page.getByTestId("notification-bell").first()).toBeVisible();
});

test("a small notification is a toast and a big one a banner; clicking opens the linked page", async ({
	page,
}) => {
	const server = await setup(page);
	await page.goto("/system/overview");

	await server.push(
		note({
			title: "bot replied",
			body: "in Main Chat",
			delivery: "small",
			link: "/system/usage",
		}),
	);
	const toast = page.getByTestId("notification-toast");
	await expect(toast).toContainText("bot replied");
	await expect(page.getByTestId("notification-unread").first()).toHaveText("1");
	await toast.getByRole("button", { name: /bot replied/ }).click();
	await expect(page).toHaveURL("/system/usage");
	await expect.poll(() => server.state.reads.length).toBeGreaterThan(0);

	await server.push(
		note({
			title: "Failed: acme/app#7",
			delivery: "big",
			link: "/system/notifications",
		}),
	);
	const banner = page.getByTestId("notification-banner");
	await expect(banner).toContainText("Failed: acme/app#7");
	await banner.getByRole("button", { name: /Failed/ }).click();
	await expect(page).toHaveURL("/system/notifications");
	await expect(banner).toHaveCount(0);
});

test("no notification is shown for the page being viewed", async ({ page }) => {
	const server = await setup(page);
	await page.goto("/system/notifications");
	await server.push(
		note({
			title: "Here already",
			link: "/system/notifications",
			delivery: "big",
		}),
	);
	await expect
		.poll(() => server.state.reads.flat())
		.toContain(server.state.history[0].id);
	await expect(page.getByTestId("notification-banner")).toHaveCount(0);
	await expect(page.getByTestId("notification-toast")).toHaveCount(0);
});

test("with the master switch off nothing is shown", async ({ page }) => {
	const server = await setup(page, { prefs: { enabled: false } });
	await stubSystemNotifications(page, "granted");
	await page.goto("/system/notifications");
	await expect(page.getByTestId("notifications-enabled")).toHaveAttribute(
		"aria-checked",
		"false",
	);
	await expect(page.getByTestId("notifications-test")).toBeDisabled();
	await page.goto("/system/overview");
	for (const delivery of ["small", "big", "system"] as const)
		await server.push(note({ delivery, link: "/system/usage" }));
	await expect(page.getByTestId("notification-unread").first()).toHaveText("3");
	await expect(page.getByTestId("notification-toast")).toHaveCount(0);
	await expect(page.getByTestId("notification-banner")).toHaveCount(0);
	expect(
		await page.evaluate(
			() => (window as unknown as { __shown: unknown[] }).__shown.length,
		),
	).toBe(0);
});

test("system delivery uses the browser when allowed", async ({ page }) => {
	const server = await setup(page);
	await stubSystemNotifications(page, "granted");
	await page.goto("/system/notifications");
	await expect(page.getByTestId("notifications-permission")).toHaveAttribute(
		"data-state",
		"granted",
	);
	await page.goto("/system/overview");
	await server.push(
		note({ title: "Workflow bot/triage failed", delivery: "system" }),
	);
	await expect
		.poll(() =>
			page.evaluate(
				() => (window as unknown as { __shown: { title: string }[] }).__shown,
			),
		)
		.toEqual([
			{
				title: "Workflow bot/triage failed",
				body: "agent exited with status 1",
			},
		]);
	await expect(page.getByTestId("notification-toast")).toHaveCount(0);
});

test("when the browser denies permission the settings say so and in-app delivery still works", async ({
	page,
}) => {
	const server = await setup(page);
	await stubSystemNotifications(page, "denied");
	await page.goto("/system/notifications");
	const permission = page.getByTestId("notifications-permission");
	await expect(permission).toHaveAttribute("data-state", "denied");
	await expect(permission).toContainText("Blocked in this browser");
	await expect(page.getByTestId("notifications-allow")).toHaveCount(0);

	await page.goto("/system/overview");
	await server.push(
		note({ title: "Falls back to a toast", delivery: "system" }),
	);
	await expect(page.getByTestId("notification-toast")).toContainText(
		"Falls back to a toast",
	);
});

test("unsupported system notifications are reported", async ({ page }) => {
	await setup(page);
	await page.addInitScript(() => {
		delete (window as unknown as { Notification?: unknown }).Notification;
	});
	await page.goto("/system/notifications");
	await expect(page.getByTestId("notifications-permission")).toHaveAttribute(
		"data-state",
		"unsupported",
	);
});

test("sound plays the notification's sound at the chosen volume, and is silent by default", async ({
	page,
}) => {
	const server = await setup(page, {
		prefs: { sound: { enabled: true, type: "chime", volume: 30 } },
	});
	await stubAudio(page);
	await page.goto("/system/overview");
	// The server leaves sound empty while sound is off.
	await server.push(note({ title: "quiet one", sound: "" }));
	await expect(page.getByTestId("notification-toast")).toHaveCount(1);
	expect(
		await page.evaluate(
			() => (window as unknown as { __played: unknown[] }).__played.length,
		),
	).toBe(0);

	await server.push(note({ title: "loud one", sound: "ping" }));
	await expect
		.poll(() =>
			page.evaluate(
				() =>
					(
						window as unknown as {
							__played: { gain: number; freqs: number[] }[];
						}
					).__played,
			),
		)
		.toEqual([{ gain: 0.12, freqs: [1568] }]);
});

test("the bell shows the unread count and history, with mark-read and clear", async ({
	page,
}) => {
	const history = [
		note({ title: "Newest", link: "/system/usage" }),
		note({ title: "Older", read: true, count: 3 }),
		note({ title: "Oldest" }),
	];
	const server = await setup(page, { history });
	await page.goto("/system/overview");
	const bell = page
		.getByTestId("sidebar-footer")
		.getByTestId("notification-bell");
	await expect(bell.getByTestId("notification-unread")).toHaveText("2");
	await expect(bell).toHaveAccessibleName("Notifications, 2 unread");

	await bell.click();
	const center = page.getByTestId("notification-center");
	const items = center.getByTestId("notification-item");
	await expect(items).toHaveCount(3);
	await expect(items.nth(1)).toContainText("×3");
	await center.getByTestId("notification-mark-all-read").click();
	await expect(bell.getByTestId("notification-unread")).toHaveCount(0);
	await expect.poll(() => server.state.reads.at(-1)).toEqual([]);

	await items.first().click();
	await expect(page).toHaveURL("/system/usage");

	await bell.click();
	await center.getByTestId("notification-clear").click();
	await expect(center.getByTestId("notification-empty")).toBeVisible();
});

test("quiet notifications reach the bell only", async ({ page }) => {
	const server = await setup(page);
	await page.goto("/system/overview");
	await server.push(note({ title: "At night", quiet: true, delivery: "big" }));
	await expect(page.getByTestId("notification-unread").first()).toHaveText("1");
	await expect(page.getByTestId("notification-banner")).toHaveCount(0);
});

test("repeated failures are suppressed by default and configurable", async ({
	page,
}) => {
	const { state } = await setup(page);
	await page.goto("/system/notifications");
	const repeats = page.getByTestId("notifications-repeats");
	await expect(
		repeats.getByTestId("notifications-repeat-suppress"),
	).toHaveAttribute("aria-checked", "true");
	await expect(repeats.getByTestId("notifications-streak")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await expect(
		repeats.getByTestId("notifications-repeat-window"),
	).toHaveAttribute("data-value", "360");
	await expect(
		repeats.getByTestId("notifications-streak-count"),
	).toHaveAttribute("data-value", "5");

	await repeats.getByTestId("notifications-repeat-window").click();
	await page.getByRole("option", { name: "1 hour" }).click();
	await expect
		.poll(
			() =>
				(state.puts.at(-1)?.repeats as { windowMinutes?: number })
					?.windowMinutes,
		)
		.toBe(60);
	await repeats.getByTestId("notifications-streak").click();
	await expect
		.poll(() => (state.puts.at(-1)?.repeats as { streak?: boolean })?.streak)
		.toBe(false);
	await expect(
		repeats.getByTestId("notifications-streak-count"),
	).toBeDisabled();
});

test("a task failing many times in a row offers Pause in its banner and the bell", async ({
	page,
}) => {
	const paused: Record<string, unknown>[] = [];
	const server = await setup(page, {
		tools: {
			task_update: (args: Record<string, unknown>) => {
				paused.push(args);
				return { ok: true };
			},
		},
	});
	await page.goto("/system/overview");
	await server.push(
		note({
			title: 'Scheduled prompt "assistant/release" has failed 5 times in a row',
			delivery: "big",
			pauseTask: "assistant/release",
		}),
	);
	const banner = page.getByTestId("notification-banner");
	await banner.getByTestId("notification-banner-pause").click();
	await expect
		.poll(() => paused)
		.toEqual([{ name: "assistant/release", enabled: false }]);
	await expect(banner).toHaveCount(0);
	await expect(page.getByTestId("notification-paused")).toContainText(
		"Paused assistant/release",
	);

	const bell = page
		.getByTestId("sidebar-footer")
		.getByTestId("notification-bell");
	await bell.click();
	const center = page.getByTestId("notification-center");
	await expect(center.getByTestId("notification-pause")).toHaveText(
		"Pause assistant/release",
	);
	await center.getByTestId("notification-pause").click();
	await expect.poll(() => paused.length).toBe(2);
});
