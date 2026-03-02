import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const AGENTS = [
	{
		id: "a1",
		name: "assistant",
		model: "anthropic/claude-sonnet-4-5",
		fallbacks: [],
		state: "idle",
	},
	{
		id: "a2",
		name: "coder",
		model: "openai/gpt-4o",
		fallbacks: [],
		state: "running",
	},
];

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
	await mockMCP(page, { agent_list: AGENTS });
});

test("renders agent cards with state badges", async ({ page }) => {
	await page.goto("/agents");

	await expect(page.getByText("assistant")).toBeVisible();
	await expect(page.getByText("coder")).toBeVisible();
	await expect(page.getByText("idle")).toBeVisible();
	await expect(page.getByText("running")).toBeVisible();
});

test("shows model in agent card", async ({ page }) => {
	await page.goto("/agents");

	await expect(page.getByText("anthropic/claude-sonnet-4-5")).toBeVisible();
	await expect(page.getByText("openai/gpt-4o")).toBeVisible();
});

test("delete shows confirm/cancel buttons", async ({ page }) => {
	await page.goto("/agents");

	// Click the first Delete button.
	await page.getByRole("button", { name: "Delete" }).first().click();

	await expect(page.getByRole("button", { name: "Confirm" })).toBeVisible();
	await expect(page.getByRole("button", { name: "Cancel" })).toBeVisible();
});

test("cancel delete restores delete button", async ({ page }) => {
	await page.goto("/agents");

	await page.getByRole("button", { name: "Delete" }).first().click();
	await expect(page.getByRole("button", { name: "Confirm" })).toBeVisible();

	await page.getByRole("button", { name: "Cancel" }).click();
	await expect(
		page.getByRole("button", { name: "Delete" }).first(),
	).toBeVisible();
	await expect(page.getByRole("button", { name: "Confirm" })).not.toBeVisible();
});

test("add agent modal opens and closes", async ({ page }) => {
	await page.goto("/agents");

	await page.getByRole("button", { name: "+ Add Agent" }).click();
	await expect(page.getByRole("heading", { name: "Add Agent" })).toBeVisible();

	// Close via Cancel.
	await page.getByRole("button", { name: "Cancel" }).click();
	await expect(
		page.getByRole("heading", { name: "Add Agent" }),
	).not.toBeVisible();
});

test("shows empty state when no agents", async ({ page }) => {
	await page.unroute("/mcp");
	await mockMCP(page, { agent_list: [] });
	await page.goto("/agents");

	await expect(page.getByText("No agents configured.")).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Add your first agent" }),
	).toBeVisible();
});
