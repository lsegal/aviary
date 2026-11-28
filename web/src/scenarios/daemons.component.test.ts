import { beforeEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { mockMCP, route } from "../component-test/server";

beforeEach(() => {
	mockMCP({ agent_list: [], job_list: [], config_validate: [] });
});

const cardsLocator = () => page.locator(".grid > div.rounded-xl");

test("daemon cards show restart controls and call restart API", async () => {
	let restartCalls = 0;

	route("/api/daemons", (r) =>
		r.fulfill({
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
		}),
	);

	route("/api/daemons/restart", (r) => {
		restartCalls += 1;
		return r.fulfill({
			status: 202,
			contentType: "application/json",
			body: JSON.stringify({ status: "restarting" }),
		});
	});

	await renderApp("/system/daemons");

	const cards = cardsLocator();
	await expect.poll(() => cards.elements().length).toBe(2);
	await expect
		.element(cards.nth(1).getByText("bot (signal 0)"))
		.toBeVisible();
	await expect
		.element(cards.nth(0).getByRole("button", { name: "Restart" }))
		.toBeVisible();
	await expect
		.element(cards.nth(1).getByRole("button", { name: "Restart" }))
		.toBeVisible();

	await cards.nth(1).getByRole("button", { name: "Restart" }).click();
	await expect.poll(() => restartCalls).toBe(1);
});

test("non-managed channels do not show empty logs panels or NaN labels", async () => {
	route("/api/daemons", (r) =>
		r.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify([
				{
					name: "ony/slack/slack-main",
					type: "slack",
					started: "2026-03-11T00:00:00Z",
					uptime: "5m",
					cpu_percent: -1,
					rss_bytes: 0,
					status: "running",
					managed: false,
				},
			]),
		}),
	);

	await renderApp("/system/daemons");

	const card = cardsLocator().first();
	await expect
		.element(card.getByText("ony (slack slack-main)"))
		.toBeVisible();
	expect(card.getByText("#NaN").elements()).toHaveLength(0);
	await expect
		.element(card.getByRole("button", { name: "Logs" }))
		.toBeVisible();
	expect(card.getByText("No output yet…").elements()).toHaveLength(0);
});

test("daemon list retries transient 500s before showing an error", async () => {
	let daemonCalls = 0;

	route("/api/daemons", (r) => {
		daemonCalls += 1;
		if (daemonCalls < 3) {
			return r.fulfill({
				status: 500,
				contentType: "text/plain",
				body: "server error",
			});
		}
		return r.fulfill({
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

	await renderApp("/system/daemons");

	await expect.element(page.getByText("Aviary Server")).toBeVisible();
	expect(page.getByText("Error: HTTP 500").elements()).toHaveLength(0);
	await expect.poll(() => daemonCalls).toBe(3);
});
