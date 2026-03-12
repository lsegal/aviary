import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
	await mockMCP(page, { agent_list: [], job_list: [], config_validate: [] });
});

test("daemon cards show restart controls and call restart API", async ({
	page,
}) => {
	let restartCalls = 0;

	await page.route("/api/daemons", async (route) => {
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify([
				{
					name: "aviary",
					type: "server",
					pid: 123,
					addr: ":16677",
					started: "2026-03-11T00:00:00Z",
					uptime: "10m",
					cpu_percent: 1.2,
					rss_bytes: 1024 * 1024,
					status: "running",
					managed: false,
				},
				{
					name: "bot/signal/0",
					type: "signal",
					pid: 456,
					started: "2026-03-11T00:00:00Z",
					uptime: "5m",
					cpu_percent: 0.4,
					rss_bytes: 2 * 1024 * 1024,
					status: "running",
					managed: true,
				},
			]),
		});
	});

	await page.route("/api/daemons/restart", async (route) => {
		restartCalls += 1;
		await route.fulfill({
			status: 202,
			contentType: "application/json",
			body: JSON.stringify({ status: "restarting" }),
		});
	});

	await page.goto("/daemons");

	const cards = page.locator(".grid > div.rounded-xl");
	await expect(cards).toHaveCount(2);
	await expect(
		cards.nth(0).getByRole("button", { name: "Restart" }),
	).toBeVisible();
	await expect(
		cards.nth(1).getByRole("button", { name: "Restart" }),
	).toBeVisible();

	await cards.nth(1).getByRole("button", { name: "Restart" }).click();
	await expect.poll(() => restartCalls).toBe(1);
});

test("daemon list retries transient 500s before showing an error", async ({
	page,
}) => {
	let daemonCalls = 0;

	await page.route("/api/daemons", async (route) => {
		daemonCalls += 1;
		if (daemonCalls < 3) {
			await route.fulfill({
				status: 500,
				contentType: "text/plain",
				body: "server error",
			});
			return;
		}

		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify([
				{
					name: "aviary",
					type: "server",
					pid: 123,
					addr: ":16677",
					started: "2026-03-11T00:00:00Z",
					uptime: "10m",
					cpu_percent: 1.2,
					rss_bytes: 1024 * 1024,
					status: "running",
					managed: false,
				},
			]),
		});
	});

	await page.goto("/daemons");

	await expect(page.getByText("Aviary Server")).toBeVisible();
	await expect(page.getByText("Error: HTTP 500")).toHaveCount(0);
	await expect.poll(() => daemonCalls).toBe(3);
});
