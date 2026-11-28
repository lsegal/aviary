import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

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
