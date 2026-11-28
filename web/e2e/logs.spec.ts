import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { chooseOption } from "./helpers/select";

/*
 * System → Logs (#1627): level, component and text filters, Clear, Load
 * previous, the reconnect banner, and long-entry truncation. The live stream
 * is a fake EventSource the test drives, so every open, line and drop happens
 * exactly when the test says; history is a routed /api/logs/history.
 */

interface Entry {
	seq: number;
	ts: string;
	level: "debug" | "info" | "warn" | "error";
	component: string;
	msg: string;
	attrs?: Record<string, string>;
	off: number;
}

const NOW = new Date().toISOString();

let nextSeq = 1;
/** A log line; lines made later sit later in the log (a higher offset). */
function entry(
	msg: string,
	opts: Partial<Omit<Entry, "msg" | "seq" | "ts" | "off">> = {},
): Entry {
	const seq = nextSeq++;
	return {
		seq,
		off: seq * 100,
		ts: NOW,
		level: "info",
		component: "server",
		msg,
		...opts,
	};
}

/** Replaces EventSource with one the test opens, feeds and drops by hand. */
async function fakeEventSource(page: Page) {
	await page.addInitScript(() => {
		const streams: FakeEventSource[] = [];
		class FakeEventSource {
			url: string;
			readyState = 0;
			onopen: ((e: Event) => void) | null = null;
			onmessage: ((e: MessageEvent) => void) | null = null;
			onerror: ((e: Event) => void) | null = null;
			constructor(url: string) {
				this.url = url;
				streams.push(this);
			}
			close() {
				this.readyState = 2;
			}
		}
		Object.assign(window, {
			EventSource: FakeEventSource,
			__logStreams: streams,
		});
	});
}

type StreamWindow = Window & {
	__logStreams: Array<{
		readyState: number;
		onopen: ((e: Event) => void) | null;
		onmessage: ((e: MessageEvent) => void) | null;
		onerror: ((e: Event) => void) | null;
	}>;
};

/** How many streams the page has opened so far. */
const streamCount = (page: Page) =>
	page.evaluate(() => (window as unknown as StreamWindow).__logStreams.length);

/** Opens the newest stream and sends it lines, as the server's tail would. */
async function openStream(page: Page, lines: Array<Entry | string> = []) {
	await page.evaluate((lines) => {
		const s = (window as unknown as StreamWindow).__logStreams.at(-1);
		if (!s) throw new Error("no log stream");
		s.readyState = 1;
		s.onopen?.(new Event("open"));
		for (const line of lines) {
			const data = typeof line === "string" ? line : JSON.stringify(line);
			s.onmessage?.(new MessageEvent("message", { data }));
		}
	}, lines);
}

/** Sends more lines on the open stream. */
async function sendLines(page: Page, lines: Entry[]) {
	await page.evaluate((lines) => {
		const s = (window as unknown as StreamWindow).__logStreams.at(-1);
		for (const line of lines)
			s?.onmessage?.(
				new MessageEvent("message", { data: JSON.stringify(line) }),
			);
	}, lines);
}

/** Drops the open stream, as a server restart or network blip would. */
async function dropStream(page: Page) {
	await page.evaluate(() => {
		const s = (window as unknown as StreamWindow).__logStreams.at(-1);
		if (s) s.readyState = 2;
		s?.onerror?.(new Event("error"));
	});
}

/** Serves history pages in order, then empty pages. */
async function routeHistory(
	page: Page,
	pages: Array<{ entries: Entry[]; hasMore: boolean }>,
) {
	const requests: string[] = [];
	await page.route(/\/api\/logs\/history\?/, (route) => {
		requests.push(route.request().url());
		const body = pages.shift() ?? { entries: [], hasMore: false };
		return route.fulfill({
			contentType: "application/json",
			body: JSON.stringify(body),
		});
	});
	return requests;
}

async function gotoLogs(page: Page, tail: Array<Entry | string> = []) {
	await fakeEventSource(page);
	await setAuthToken(page);
	await mockMCP(page, { config_validate: [], agent_list: [] });
	await page.goto("/system/logs");
	await expect.poll(() => streamCount(page)).toBe(1);
	await openStream(page, tail);
}

const rows = (page: Page) => page.getByTestId("log-row");

/** The shown rows' messages, top to bottom. */
const messages = (page: Page) =>
	rows(page).evaluateAll((els) =>
		els.map((el) => el.children[3]?.textContent?.trim().split(/\s+/)[0] ?? ""),
	);

const levelSelect = (page: Page) =>
	page.getByRole("combobox", { name: "Log level" });

const chip = (page: Page, name: string) =>
	page.getByRole("button", { name, exact: true });

test("the level filter hides lower levels and starts at INFO+", async ({
	page,
}) => {
	await gotoLogs(page, [
		entry("debug-line", { level: "debug" }),
		entry("info-line"),
		entry("warn-line", { level: "warn" }),
		entry("error-line", { level: "error" }),
		// The server reports a non-JSON log line as an info line from "log".
		entry("plain-text-line", { component: "log" }),
	]);

	await expect
		.poll(() => messages(page))
		.toEqual(["info-line", "warn-line", "error-line", "plain-text-line"]);
	await expect(chip(page, "log")).toBeVisible();

	await chooseOption(levelSelect(page), "DEBUG+");
	await expect
		.poll(() => messages(page))
		.toEqual([
			"debug-line",
			"info-line",
			"warn-line",
			"error-line",
			"plain-text-line",
		]);

	await chooseOption(levelSelect(page), "WARN+");
	await expect.poll(() => messages(page)).toEqual(["warn-line", "error-line"]);

	await chooseOption(levelSelect(page), "ERROR");
	await expect.poll(() => messages(page)).toEqual(["error-line"]);

	await chooseOption(levelSelect(page), "INFO+");
	await expect(rows(page)).toHaveCount(4);
});

test("component chips filter rows by one or several components", async ({
	page,
}) => {
	await gotoLogs(page, [
		entry("server-line"),
		entry("agent-line", { component: "agent" }),
		entry("sched-line", { component: "scheduler" }),
	]);
	await expect(rows(page)).toHaveCount(3);

	// A chip appears for each component seen, including ones that arrive live.
	await expect(chip(page, "agent")).toBeVisible();
	await expect(chip(page, "scheduler")).toBeVisible();
	await expect(chip(page, "server")).toBeVisible();
	await expect(chip(page, "mcp")).toHaveCount(0);
	await sendLines(page, [entry("mcp-line", { component: "mcp" })]);
	await expect(chip(page, "mcp")).toBeVisible();

	await chip(page, "agent").click();
	await expect.poll(() => messages(page)).toEqual(["agent-line"]);

	await chip(page, "scheduler").click();
	await expect.poll(() => messages(page)).toEqual(["agent-line", "sched-line"]);

	await chip(page, "agent").click();
	await expect.poll(() => messages(page)).toEqual(["sched-line"]);

	// Turning every chip off shows everything again.
	await chip(page, "scheduler").click();
	await expect
		.poll(() => messages(page))
		.toEqual(["server-line", "agent-line", "sched-line", "mcp-line"]);
});

test("the text filter matches component, message and attrs, and combines with the other filters", async ({
	page,
}) => {
	await gotoLogs(page, [
		entry("starting", { component: "scheduler" }),
		entry("ran", { component: "scheduler", attrs: { job: "Nightly" } }),
		entry("failed", {
			component: "scheduler",
			level: "warn",
			attrs: { job: "nightly" },
		}),
		entry("Nightly-backup", { component: "agent" }),
		entry("unrelated"),
	]);
	await expect(rows(page)).toHaveCount(5);
	const search = page.getByPlaceholder("Filter…");

	// Component, case-insensitively.
	await search.fill("SCHED");
	await expect
		.poll(() => messages(page))
		.toEqual(["starting", "ran", "failed"]);

	// Message, case-insensitively.
	await search.fill("BACKUP");
	await expect.poll(() => messages(page)).toEqual(["Nightly-backup"]);

	// key=value attrs, case-insensitively.
	await search.fill("JOB=nightly");
	await expect.poll(() => messages(page)).toEqual(["ran", "failed"]);

	// Combined with a chip and the level filter.
	await search.fill("nightly");
	await expect
		.poll(() => messages(page))
		.toEqual(["ran", "failed", "Nightly-backup"]);
	await chip(page, "scheduler").click();
	await expect.poll(() => messages(page)).toEqual(["ran", "failed"]);
	await chooseOption(levelSelect(page), "WARN+");
	await expect.poll(() => messages(page)).toEqual(["failed"]);

	await search.fill("no-such-text");
	await expect(rows(page)).toHaveCount(0);
	await expect(page.getByText("No log entries yet.")).toBeVisible();
});

test("Clear empties the list and Load previous brings back only older history", async ({
	page,
}) => {
	const older = [entry("older-1"), entry("older-2"), entry("older-3")];
	const history = await routeHistory(page, [
		{ entries: older, hasMore: false },
	]);
	const tail = [entry("tail-1"), entry("tail-2")];
	await gotoLogs(page, tail);
	await expect.poll(() => messages(page)).toEqual(["tail-1", "tail-2"]);

	await page.getByRole("button", { name: "Clear" }).click();
	await expect(rows(page)).toHaveCount(0);
	await expect(page.getByText("No log entries yet.")).toBeVisible();
	await expect(chip(page, "server")).toHaveCount(0);

	// History pages back from where the cleared lines started, so the
	// cleared tail does not come back.
	await page.getByRole("button", { name: /Load previous/ }).click();
	await expect
		.poll(() => messages(page))
		.toEqual(["older-1", "older-2", "older-3"]);
	expect(history).toHaveLength(1);
	expect(new URL(history[0]).searchParams.get("before")).toBe(
		String(tail[0].off),
	);
	// No more history: the button goes away.
	await expect(page.getByRole("button", { name: /Load previous/ })).toHaveCount(
		0,
	);

	// Lines that arrive afterwards still show, below the history.
	await sendLines(page, [entry("after-clear")]);
	await expect
		.poll(() => messages(page))
		.toEqual(["older-1", "older-2", "older-3", "after-clear"]);
});

test("Load previous after live growth prepends older pages in order", async ({
	page,
}) => {
	// Made oldest first, so offsets increase down the log.
	const page2 = Array.from({ length: 3 }, (_, i) => entry(`hist-a${i}`));
	const page1 = Array.from({ length: 3 }, (_, i) => entry(`hist-b${i}`));
	const history = await routeHistory(page, [
		{ entries: page1, hasMore: true },
		{ entries: page2, hasMore: false },
	]);
	const tail = [entry("tail-1"), entry("tail-2")];
	await gotoLogs(page, tail);
	await sendLines(page, [entry("live-1"), entry("live-2")]);
	await expect
		.poll(() => messages(page))
		.toEqual(["tail-1", "tail-2", "live-1", "live-2"]);

	const loadPrevious = page.getByRole("button", { name: /Load previous/ });
	await expect(loadPrevious).toHaveText("↑ Load previous 200 lines");
	await loadPrevious.click();
	await expect
		.poll(() => messages(page))
		.toEqual([
			"hist-b0",
			"hist-b1",
			"hist-b2",
			"tail-1",
			"tail-2",
			"live-1",
			"live-2",
		]);

	// More lines arrive live between pages; they stay at the bottom.
	await sendLines(page, [entry("live-3")]);
	await loadPrevious.click();
	await expect
		.poll(() => messages(page))
		.toEqual([
			"hist-a0",
			"hist-a1",
			"hist-a2",
			"hist-b0",
			"hist-b1",
			"hist-b2",
			"tail-1",
			"tail-2",
			"live-1",
			"live-2",
			"live-3",
		]);
	// Each page asks for the lines before the oldest one shown.
	expect(history.map((u) => new URL(u).searchParams.get("before"))).toEqual([
		String(tail[0].off),
		String(page1[0].off),
	]);
	await expect(loadPrevious).toHaveCount(0);
});

test("a dropped stream shows the reconnect banner until the stream reopens", async ({
	page,
}) => {
	await gotoLogs(page, [entry("before-drop")]);
	const banner = page.getByText("Log stream disconnected — reconnecting…");
	await expect(page.getByTitle("Live")).toHaveClass(/bg-green-500/);
	await expect(banner).toHaveCount(0);

	await dropStream(page);
	await expect(banner).toBeVisible();
	await expect(page.getByTitle("Reconnecting…")).toHaveClass(/bg-red-400/);
	await expect(page.getByTitle("Live")).toHaveCount(0);

	// The page reconnects on its own after a short backoff.
	await expect.poll(() => streamCount(page)).toBe(2);
	await openStream(page, [entry("after-reconnect")]);
	await expect(banner).toHaveCount(0);
	await expect(page.getByTitle("Live")).toHaveClass(/bg-green-500/);
	await expect(rows(page).filter({ hasText: "after-reconnect" })).toHaveCount(
		1,
	);
});

test("long entries truncate with a +N bytes button that expands and collapses", async ({
	page,
}) => {
	const msg = `long-entry ${"x".repeat(2400)}`;
	await gotoLogs(page, [entry(msg, { attrs: { tail: "END-MARK" } })]);
	const row = rows(page).first();
	const fullLength = `${msg} tail=END-MARK`.length;

	const expand = row.getByRole("button", {
		name: `+${fullLength - 2000} bytes`,
	});
	await expect(expand).toBeVisible();
	await expect(row).not.toContainText("END-MARK");

	await expand.click();
	await expect(row).toContainText("tail=END-MARK");
	const collapse = row.getByRole("button", { name: "collapse" });
	await expect(collapse).toBeVisible();

	await collapse.click();
	await expect(row).not.toContainText("END-MARK");
	await expect(expand).toBeVisible();

	// Short entries get neither button.
	await sendLines(page, [entry("short-entry")]);
	const short = rows(page).filter({ hasText: "short-entry" });
	await expect(short).toHaveCount(1);
	await expect(short.getByRole("button")).toHaveCount(0);
});

const LINE_BYTES = 100;

interface FakeLine {
	seq: number;
	ts: string;
	level: "info";
	component: string;
	msg: string;
	off: number;
}

// A log file: every line carries its byte offset, as the server sends it.
function fakeLog(messages: string[]): FakeLine[] {
	return messages.map((msg, i) => ({
		seq: i + 1,
		ts: NOW,
		level: "info",
		component: "server",
		msg,
		off: i * LINE_BYTES,
	}));
}

function range(prefix: string, from: number, to: number): string[] {
	return Array.from(
		{ length: to - from + 1 },
		(_, i) => `${prefix} ${from + i}`,
	);
}

// Serves the SSE tail of the log as it stood when the page connected, then
// the lines appended since, and /api/logs/history the way the server pages
// it: the latest limit lines before the given offset, or before the current
// end of the log without one.
async function mockLogs(
	page: Page,
	log: FakeLine[],
	tailed: number,
	grown: number,
) {
	await page.route(/\/api\/logs\/history\?/, (route) => {
		const url = new URL(route.request().url());
		const limit = Number(url.searchParams.get("limit") ?? 500);
		const raw = url.searchParams.get("before");
		const before = raw === null ? Number.POSITIVE_INFINITY : Number(raw);
		const older = log.filter((l) => l.off < before);
		const entries = older.slice(-limit);
		return route.fulfill({
			contentType: "application/json",
			body: JSON.stringify({
				entries,
				hasMore: older.length > entries.length,
			}),
		});
	});
	// One stream; a reconnect waits forever so the tail is not re-sent.
	let streams = 0;
	await page.route(/\/api\/logs\?/, (route) => {
		if (streams++) return;
		const live = log.slice(log.length - grown - tailed);
		return route.fulfill({
			contentType: "text/event-stream",
			body: live.map((l) => `data: ${JSON.stringify(l)}\n\n`).join(""),
		});
	});
	await setAuthToken(page);
	await mockMCP(page, { config_validate: [], agent_list: [] });
}

function shownMessages(page: Page): Promise<string[]> {
	return page
		.getByTestId("logs-scroll")
		.locator(":scope > div.border-b > span:last-child")
		.allTextContents()
		.then((texts) => texts.map((t) => t.trim()));
}

function expectNoDuplicates(messages: string[]) {
	const seen = new Set<string>();
	const dupes = messages.filter((m) => seen.size === seen.add(m).size);
	expect(dupes).toEqual([]);
}

test("Load previous adds only older lines after the log grows (#1624)", async ({
	page,
}) => {
	// 1,000 lines when the page opens (the tail shows the last 200), then 300
	// more stream in.
	const log = fakeLog([...range("OLD", 1, 1000), ...range("GROW", 1, 300)]);
	await mockLogs(page, log, 200, 300);
	await page.goto("/system/logs");
	const scroller = page.getByTestId("logs-scroll");
	await expect(scroller.getByText("GROW 300", { exact: true })).toBeAttached();
	await expect.poll(async () => (await shownMessages(page)).length).toBe(500);

	await scroller.getByRole("button", { name: /Load previous/ }).click();
	await expect.poll(async () => (await shownMessages(page)).length).toBe(700);
	const messages = await shownMessages(page);
	expectNoDuplicates(messages);
	expect(messages).toEqual([
		...range("OLD", 601, 1000),
		...range("GROW", 1, 300),
	]);
});

test("Load previous keeps older pages once the buffer is trimmed (#1624)", async ({
	page,
}) => {
	// 1,900 live lines push the buffer past its cap, so it keeps the newest
	// 1,800 (BULK 101 onward).
	const log = fakeLog([...range("OLD", 1, 1000), ...range("BULK", 1, 1900)]);
	await mockLogs(page, log, 200, 1900);
	await page.goto("/system/logs");
	const scroller = page.getByTestId("logs-scroll");
	await expect(scroller.getByText("BULK 1900", { exact: true })).toBeAttached();
	await expect
		.poll(async () => (await shownMessages(page))[0])
		.toBe("BULK 101");

	const loadPrevious = scroller.getByRole("button", { name: /Load previous/ });
	await loadPrevious.click();
	await expect.poll(async () => (await shownMessages(page)).length).toBe(2000);
	let messages = await shownMessages(page);
	expectNoDuplicates(messages);
	expect(messages).toEqual([
		...range("OLD", 901, 1000),
		...range("BULK", 1, 1900),
	]);

	// A second page is kept too, rather than trimmed straight back off.
	await loadPrevious.click();
	await expect.poll(async () => (await shownMessages(page)).length).toBe(2200);
	messages = await shownMessages(page);
	expectNoDuplicates(messages);
	expect(messages.slice(0, 3)).toEqual(["OLD 701", "OLD 702", "OLD 703"]);
	expect(messages.at(-1)).toBe("BULK 1900");
});
