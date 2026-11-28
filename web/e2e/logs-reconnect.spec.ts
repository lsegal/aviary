import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Every /api/logs connection re-sends the tail numbered from 1, so a
// reconnect must replace the shown rows, not append a second copy (#1625).
test("a Logs stream reconnect does not duplicate the tail (#1625)", async ({
	page,
}) => {
	await setAuthToken(page);
	await mockMCP(page);
	const tail = Array.from({ length: 5 }, (_, i) => ({
		seq: i + 1,
		ts: "2026-11-27T12:00:00Z",
		level: "info",
		component: "server",
		msg: `line ${i + 1}`,
	}));
	let connections = 0;
	await page.route(/\/api\/logs\?/, (route) => {
		connections++;
		const lines =
			connections === 1
				? tail
				: [...tail, { ...tail[0], seq: 6, msg: "line 6" }];
		// Ending the body closes the stream, so the page reconnects.
		return route.fulfill({
			status: 200,
			headers: { "Content-Type": "text/event-stream" },
			body: lines.map((e) => `data: ${JSON.stringify(e)}\n\n`).join(""),
		});
	});

	await page.goto("/system/logs");
	const rows = page.getByTestId("log-row");
	await expect.poll(() => connections).toBeGreaterThanOrEqual(2);
	await expect(rows.filter({ hasText: "line 6" })).toHaveCount(1);
	await expect(rows).toHaveCount(6);
	for (let i = 1; i <= 6; i++) {
		await expect(rows.filter({ hasText: `line ${i}` })).toHaveCount(1);
	}
});
