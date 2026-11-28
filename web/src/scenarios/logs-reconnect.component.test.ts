import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { eventSourcesOpened, mockMCP } from "../component-test/server";

const logStreams = () =>
	eventSourcesOpened().filter((s) => new URL(s.url).pathname === "/api/logs");

// Every /api/logs connection re-sends the tail numbered from 1, so a
// reconnect must replace the shown rows, not append a second copy (#1625).
test("a Logs stream reconnect does not duplicate the tail (#1625)", async () => {
	mockMCP();
	const tail = Array.from({ length: 5 }, (_, i) => ({
		seq: i + 1,
		ts: "2026-11-27T12:00:00Z",
		level: "info",
		component: "server",
		msg: `line ${i + 1}`,
	}));

	await renderApp("/system/logs");
	const rows = page.getByTestId("log-row");
	const count = (text: string) =>
		rows.filter({ hasText: text }).elements().length;

	// The first connection sends the tail, then the stream ends, as the server
	// closing the body would, so the page reconnects.
	await expect.poll(() => logStreams()[0]?.readyState).toBe(1);
	for (const e of tail) logStreams()[0].emit(JSON.stringify(e));
	await expect.poll(() => rows.elements().length).toBe(5);
	const first = logStreams()[0];
	first.readyState = 2;
	const error = new Event("error");
	first.onerror?.(error);
	first.dispatchEvent(error);

	// The reconnect re-sends the tail plus a new line.
	await expect.poll(() => logStreams().length).toBeGreaterThanOrEqual(2);
	await expect.poll(() => logStreams()[1].readyState).toBe(1);
	for (const e of [...tail, { ...tail[0], seq: 6, msg: "line 6" }])
		logStreams()[1].emit(JSON.stringify(e));

	await expect.poll(() => count("line 6")).toBe(1);
	await expect.poll(() => rows.elements().length).toBe(6);
	for (let i = 1; i <= 6; i++) expect(count(`line ${i}`)).toBe(1);
});
