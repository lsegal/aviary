import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { eventSourcesOpened, mockMCP, route } from "../component-test/server";
import { chooseOption } from "./helpers/select";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/LogsView.vue"));

/*
 * System → Logs (#1627): level, component and text filters, Clear, Load
 * previous, the reconnect banner, and long-entry truncation. The live stream
 * is the harness's fake EventSource, which the test feeds and drops by hand;
 * history is a routed /api/logs/history.
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

/** The page's log streams, oldest first. */
const logStreams = () =>
	eventSourcesOpened().filter((s) => new URL(s.url).pathname === "/api/logs");

/** How many streams the page has opened so far. */
const streamCount = () => logStreams().length;

/** Waits for the newest stream to open, then sends it lines, as the server's
 * tail would. */
async function openStream(lines: Array<Entry | string> = []) {
	await expect.poll(() => logStreams().at(-1)?.readyState).toBe(1);
	sendLines(lines);
}

/** Sends more lines on the open stream. */
function sendLines(lines: Array<Entry | string>) {
	const s = logStreams().at(-1);
	if (!s) throw new Error("no log stream");
	for (const line of lines)
		s.emit(typeof line === "string" ? line : JSON.stringify(line));
}

/** Drops the open stream, as a server restart or network blip would. */
function dropStream() {
	const s = logStreams().at(-1);
	if (!s) throw new Error("no log stream");
	s.readyState = 2;
	const e = new Event("error");
	s.onerror?.(e);
	s.dispatchEvent(e);
}

/** Serves history pages in order, then empty pages. */
function routeHistory(pages: Array<{ entries: Entry[]; hasMore: boolean }>) {
	const requests: string[] = [];
	route(/\/api\/logs\/history\?/, (r) => {
		requests.push(r.request().url());
		const body = pages.shift() ?? { entries: [], hasMore: false };
		return r.fulfill({ json: body });
	});
	return requests;
}

async function gotoLogs(tail: Array<Entry | string> = []) {
	mockMCP({ config_validate: [], agent_list: [] });
	await renderApp("/system/logs");
	await expect.poll(streamCount).toBe(1);
	await openStream(tail);
}

const rows = () => page.getByTestId("log-row");
const rowCount = () => rows().elements().length;

/** The shown rows' messages, top to bottom. */
const messages = () =>
	rows()
		.elements()
		.map((el) => el.children[3]?.textContent?.trim().split(/\s+/)[0] ?? "");

const levelSelect = () => page.getByRole("combobox", { name: "Log level" });

const chip = (name: string) => page.getByRole("button", { name, exact: true });
const chipCount = (name: string) => chip(name).elements().length;

test("the level filter hides lower levels and starts at INFO+", async () => {
	await gotoLogs([
		entry("debug-line", { level: "debug" }),
		entry("info-line"),
		entry("warn-line", { level: "warn" }),
		entry("error-line", { level: "error" }),
		// The server reports a non-JSON log line as an info line from "log".
		entry("plain-text-line", { component: "log" }),
	]);

	await expect
		.poll(messages)
		.toEqual(["info-line", "warn-line", "error-line", "plain-text-line"]);
	await expect.element(chip("log")).toBeVisible();

	await chooseOption(levelSelect(), "DEBUG+");
	await expect
		.poll(messages)
		.toEqual([
			"debug-line",
			"info-line",
			"warn-line",
			"error-line",
			"plain-text-line",
		]);

	await chooseOption(levelSelect(), "WARN+");
	await expect.poll(messages).toEqual(["warn-line", "error-line"]);

	await chooseOption(levelSelect(), "ERROR");
	await expect.poll(messages).toEqual(["error-line"]);

	await chooseOption(levelSelect(), "INFO+");
	await expect.poll(rowCount).toBe(4);
});

test("component chips filter rows by one or several components", async () => {
	await gotoLogs([
		entry("server-line"),
		entry("agent-line", { component: "agent" }),
		entry("sched-line", { component: "scheduler" }),
	]);
	await expect.poll(rowCount).toBe(3);

	// A chip appears for each component seen, including ones that arrive live.
	await expect.element(chip("agent")).toBeVisible();
	await expect.element(chip("scheduler")).toBeVisible();
	await expect.element(chip("server")).toBeVisible();
	expect(chipCount("mcp")).toBe(0);
	sendLines([entry("mcp-line", { component: "mcp" })]);
	await expect.element(chip("mcp")).toBeVisible();

	await chip("agent").click();
	await expect.poll(messages).toEqual(["agent-line"]);

	await chip("scheduler").click();
	await expect.poll(messages).toEqual(["agent-line", "sched-line"]);

	await chip("agent").click();
	await expect.poll(messages).toEqual(["sched-line"]);

	// Turning every chip off shows everything again.
	await chip("scheduler").click();
	await expect
		.poll(messages)
		.toEqual(["server-line", "agent-line", "sched-line", "mcp-line"]);
});

test("the text filter matches component, message and attrs, and combines with the other filters", async () => {
	await gotoLogs([
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
	await expect.poll(rowCount).toBe(5);
	const search = page.getByPlaceholder("Filter…");

	// Component, case-insensitively.
	await search.fill("SCHED");
	await expect.poll(messages).toEqual(["starting", "ran", "failed"]);

	// Message, case-insensitively.
	await search.fill("BACKUP");
	await expect.poll(messages).toEqual(["Nightly-backup"]);

	// key=value attrs, case-insensitively.
	await search.fill("JOB=nightly");
	await expect.poll(messages).toEqual(["ran", "failed"]);

	// Combined with a chip and the level filter.
	await search.fill("nightly");
	await expect.poll(messages).toEqual(["ran", "failed", "Nightly-backup"]);
	await chip("scheduler").click();
	await expect.poll(messages).toEqual(["ran", "failed"]);
	await chooseOption(levelSelect(), "WARN+");
	await expect.poll(messages).toEqual(["failed"]);

	await search.fill("no-such-text");
	await expect.poll(rowCount).toBe(0);
	await expect.element(page.getByText("No log entries yet.")).toBeVisible();
});

test("Clear empties the list and Load previous brings back only older history", async () => {
	const older = [entry("older-1"), entry("older-2"), entry("older-3")];
	const history = routeHistory([{ entries: older, hasMore: false }]);
	const tail = [entry("tail-1"), entry("tail-2")];
	await gotoLogs(tail);
	await expect.poll(messages).toEqual(["tail-1", "tail-2"]);

	await page.getByRole("button", { name: "Clear" }).click();
	await expect.poll(rowCount).toBe(0);
	await expect.element(page.getByText("No log entries yet.")).toBeVisible();
	await expect.poll(() => chipCount("server")).toBe(0);

	// History pages back from where the cleared lines started, so the
	// cleared tail does not come back.
	const loadPrevious = page.getByRole("button", { name: /Load previous/ });
	await loadPrevious.click();
	await expect.poll(messages).toEqual(["older-1", "older-2", "older-3"]);
	expect(history).toHaveLength(1);
	expect(new URL(history[0]).searchParams.get("before")).toBe(
		String(tail[0].off),
	);
	// No more history: the button goes away.
	await expect.poll(() => loadPrevious.elements().length).toBe(0);

	// Lines that arrive afterwards still show, below the history.
	sendLines([entry("after-clear")]);
	await expect
		.poll(messages)
		.toEqual(["older-1", "older-2", "older-3", "after-clear"]);
});

test("Load previous after live growth prepends older pages in order", async () => {
	// Made oldest first, so offsets increase down the log.
	const page2 = Array.from({ length: 3 }, (_, i) => entry(`hist-a${i}`));
	const page1 = Array.from({ length: 3 }, (_, i) => entry(`hist-b${i}`));
	const history = routeHistory([
		{ entries: page1, hasMore: true },
		{ entries: page2, hasMore: false },
	]);
	const tail = [entry("tail-1"), entry("tail-2")];
	await gotoLogs(tail);
	sendLines([entry("live-1"), entry("live-2")]);
	await expect.poll(messages).toEqual(["tail-1", "tail-2", "live-1", "live-2"]);

	const loadPrevious = page.getByRole("button", { name: /Load previous/ });
	await expect.element(loadPrevious).toHaveText("↑ Load previous 200 lines");
	await loadPrevious.click();
	await expect
		.poll(messages)
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
	sendLines([entry("live-3")]);
	await loadPrevious.click();
	await expect
		.poll(messages)
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
	await expect.poll(() => loadPrevious.elements().length).toBe(0);
});

test("a dropped stream shows the reconnect banner until the stream reopens", async () => {
	await gotoLogs([entry("before-drop")]);
	const banner = page.getByText("Log stream disconnected — reconnecting…");
	await expect.element(page.getByTitle("Live")).toHaveClass(/bg-green-500/);
	expect(banner.elements()).toHaveLength(0);

	dropStream();
	await expect.element(banner).toBeVisible();
	await expect
		.element(page.getByTitle("Reconnecting…"))
		.toHaveClass(/bg-red-400/);
	expect(page.getByTitle("Live").elements()).toHaveLength(0);

	// The page reconnects on its own after a short backoff.
	await expect.poll(streamCount).toBe(2);
	await openStream([entry("after-reconnect")]);
	await expect.poll(() => banner.elements().length).toBe(0);
	await expect.element(page.getByTitle("Live")).toHaveClass(/bg-green-500/);
	await expect
		.poll(() => rows().filter({ hasText: "after-reconnect" }).elements().length)
		.toBe(1);
});

test("long entries truncate with a +N bytes button that expands and collapses", async () => {
	const msg = `long-entry ${"x".repeat(2400)}`;
	await gotoLogs([entry(msg, { attrs: { tail: "END-MARK" } })]);
	const row = rows().first();
	const fullLength = `${msg} tail=END-MARK`.length;

	const expand = row.getByRole("button", {
		name: `+${fullLength - 2000} bytes`,
	});
	await expect.element(expand).toBeVisible();
	await expect.element(row).not.toMatchTextContent("END-MARK");

	await expand.click();
	await expect.element(row).toMatchTextContent("tail=END-MARK");
	const collapse = row.getByRole("button", { name: "collapse" });
	await expect.element(collapse).toBeVisible();

	await collapse.click();
	await expect.element(row).not.toMatchTextContent("END-MARK");
	await expect.element(expand).toBeVisible();

	// Short entries get neither button.
	sendLines([entry("short-entry")]);
	const short = rows().filter({ hasText: "short-entry" });
	await expect.poll(() => short.elements().length).toBe(1);
	expect(short.getByRole("button").elements()).toHaveLength(0);
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

// Serves /api/logs/history the way the server pages it: the latest limit
// lines before the given offset, or before the current end of the log
// without one.
function mockLogs(log: FakeLine[]) {
	route(/\/api\/logs\/history\?/, (r) => {
		const url = new URL(r.request().url());
		const limit = Number(url.searchParams.get("limit") ?? 500);
		const raw = url.searchParams.get("before");
		const before = raw === null ? Number.POSITIVE_INFINITY : Number(raw);
		const older = log.filter((l) => l.off < before);
		const entries = older.slice(-limit);
		return r.fulfill({
			json: { entries, hasMore: older.length > entries.length },
		});
	});
	mockMCP({ config_validate: [], agent_list: [] });
}

// Streams the tail of the log as it stood when the page connected, then the
// lines appended since, over the page's one log stream.
async function streamLog(log: FakeLine[], tailed: number, grown: number) {
	await expect
		.poll(() =>
			eventSourcesOpened().find(
				(es) => new URL(es.url).pathname === "/api/logs" && es.readyState === 1,
			),
		)
		.toBeTruthy();
	const es = eventSourcesOpened().find(
		(s) => new URL(s.url).pathname === "/api/logs",
	);
	for (const l of log.slice(log.length - grown - tailed))
		es?.emit(JSON.stringify(l));
}

function shownMessages(): string[] {
	return page
		.getByTestId("logs-scroll")
		.locator(":scope > div.border-b > span:last-child")
		.elements()
		.map((el) => (el.textContent ?? "").trim());
}

function expectNoDuplicates(messages: string[]) {
	const seen = new Set<string>();
	const dupes = messages.filter((m) => seen.size === seen.add(m).size);
	expect(dupes).toEqual([]);
}

test("Load previous adds only older lines after the log grows (#1624)", async () => {
	// 1,000 lines when the page opens (the tail shows the last 200), then 300
	// more stream in.
	const log = fakeLog([...range("OLD", 1, 1000), ...range("GROW", 1, 300)]);
	mockLogs(log);
	await renderApp("/system/logs");
	await streamLog(log, 200, 300);
	const scroller = page.getByTestId("logs-scroll");
	await expect
		.element(scroller.getByText("GROW 300", { exact: true }))
		.toBeInTheDocument();
	await expect.poll(() => shownMessages().length).toBe(500);

	await scroller.getByRole("button", { name: /Load previous/ }).click();
	await expect.poll(() => shownMessages().length).toBe(700);
	const messages = shownMessages();
	expectNoDuplicates(messages);
	expect(messages).toEqual([
		...range("OLD", 601, 1000),
		...range("GROW", 1, 300),
	]);
});

test("Load previous keeps older pages once the buffer is trimmed (#1624)", async () => {
	// 1,900 live lines push the buffer past its cap, so it keeps the newest
	// 1,800 (BULK 101 onward).
	const log = fakeLog([...range("OLD", 1, 1000), ...range("BULK", 1, 1900)]);
	mockLogs(log);
	await renderApp("/system/logs");
	await streamLog(log, 200, 1900);
	const scroller = page.getByTestId("logs-scroll");
	await expect
		.element(scroller.getByText("BULK 1900", { exact: true }))
		.toBeInTheDocument();
	await expect.poll(() => shownMessages()[0]).toBe("BULK 101");

	const loadPrevious = scroller.getByRole("button", { name: /Load previous/ });
	await loadPrevious.click();
	await expect.poll(() => shownMessages().length).toBe(2000);
	let messages = shownMessages();
	expectNoDuplicates(messages);
	expect(messages).toEqual([
		...range("OLD", 901, 1000),
		...range("BULK", 1, 1900),
	]);

	// A second page is kept too, rather than trimmed straight back off.
	await loadPrevious.click();
	await expect.poll(() => shownMessages().length).toBe(2200);
	messages = shownMessages();
	expectNoDuplicates(messages);
	expect(messages.slice(0, 3)).toEqual(["OLD 701", "OLD 702", "OLD 703"]);
	expect(messages.at(-1)).toBe("BULK 1900");
});
