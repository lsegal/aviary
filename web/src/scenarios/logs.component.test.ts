import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import {
	eventSourcesOpened,
	mockMCP,
	route,
} from "../component-test/server";

const NOW = new Date().toISOString();
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
		// A chained .locator() recurses in the harness, so one selector.
		.locator('[data-testid="logs-scroll"] > div.border-b > span:last-child')
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
