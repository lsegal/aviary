import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

interface MockConfig {
	server?: Record<string, unknown>;
	agents: Record<string, unknown>[];
}

// config_get answers from the last saved config, so saves show on reload.
async function mockConfig(page: Page, initial: MockConfig) {
	const state = { config: initial, saves: [] as MockConfig[] };
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: (args) =>
			args?.agent === "assistant"
				? [{ id: "main", agent_id: "assistant", name: "main" }]
				: [],
		config_get: () => state.config,
		config_save: (args) => {
			state.config = JSON.parse(String(args?.config ?? "{}"));
			state.saves.push(state.config);
			return "saved";
		},
	});
	return state;
}

const twoAgents: MockConfig = {
	agents: [
		{
			name: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			tasks: [{ name: "daily", schedule: "0 9 * * *", prompt: "hi" }],
			projects: [{ target: "lsegal/aviary" }],
		},
		{ name: "helper", model: "openai/gpt-5" },
	],
};

/** The headings of System → Settings' sections, top to bottom. */
async function sectionHeadings(page: Page) {
	return page
		.locator("section h3")
		.evaluateAll((els) =>
			els
				.filter((el) => (el as HTMLElement).offsetParent)
				.map((el) => (el.textContent ?? "").trim()),
		);
}

test("the sidebar header has no + and the title is uncovered", async ({
	page,
}) => {
	await mockConfig(page, { agents: [{ name: "assistant", model: "" }] });
	await page.goto("/system/overview");

	const title = page.getByTestId("sidebar-title");
	await expect(title).toHaveText("Aviary");
	await expect(page.getByTestId("sidebar-add-agent")).toHaveCount(0);
	// Nothing is drawn over the title's centre.
	const covered = await title.evaluate((el) => {
		const r = el.getBoundingClientRect();
		const hit = document.elementFromPoint(
			r.left + r.width / 2,
			r.top + r.height / 2,
		);
		return hit !== el && !el.contains(hit);
	});
	expect(covered).toBe(false);

	// Collapsed, the rail's hover overlay shows the same header.
	await page.getByTestId("sidebar-collapse").click();
	await page.getByTestId("sidebar-rail").hover();
	await expect(page.getByTestId("sidebar")).toHaveAttribute("data-peek", "true");
	await expect(page.getByTestId("sidebar-title")).toHaveText("Aviary");
	await expect(page.getByTestId("sidebar-add-agent")).toHaveCount(0);
});

test("Agents sits directly below Models and Server is last", async ({
	page,
}) => {
	await mockConfig(page, twoAgents);
	await page.goto("/system/settings");

	const headings = await sectionHeadings(page);
	expect(headings.indexOf("Agents")).toBe(headings.indexOf("Models") + 1);
	expect(headings.at(-1)).toBe("Server");

	const rows = page.getByTestId("settings-agent-row");
	await expect(rows.getByTestId("settings-agent-name")).toHaveText([
		"assistant",
		"helper",
	]);
	await expect(rows.first()).toContainText("anthropic/claude-sonnet-4-5");
	await expect(rows.first().getByTestId("settings-agent-name")).toHaveAttribute(
		"href",
		"/agents/assistant/settings",
	);
});

test("+ Add agent opens the create-agent flow", async ({ page }) => {
	await mockConfig(page, twoAgents);
	await page.goto("/system/settings");
	await page.getByTestId("settings-add-agent").click();
	await expect(page).toHaveURL("/agents/_3/settings");
	await page.locator('input[placeholder="assistant"]').fill("scout");
	await page.getByRole("button", { name: "Save Changes" }).click();

	await page.goto("/system/settings");
	await expect(
		page.getByTestId("settings-agent-row").getByTestId("settings-agent-name"),
	).toHaveText(["assistant", "helper", "scout"]);
});

test("Rename rejects empty and duplicate names and saves the new name", async ({
	page,
}) => {
	const state = await mockConfig(page, twoAgents);
	await page.goto("/system/settings");

	const row = page.locator('[data-testid=settings-agent-row][data-agent="assistant"]');
	await row.getByTestId("settings-agent-menu").click();
	await page.getByTestId("settings-agent-rename").click();
	const input = page.getByTestId("rename-agent-input");
	await expect(input).toHaveValue("assistant");

	await input.fill("  ");
	await page.getByTestId("rename-agent-confirm").click();
	await expect(page.getByTestId("rename-agent-error")).toHaveText("Enter a name.");

	await input.fill("Helper");
	await page.getByTestId("rename-agent-confirm").click();
	await expect(page.getByTestId("rename-agent-error")).toContainText(
		"already exists",
	);
	expect(state.saves).toHaveLength(0);

	await input.fill("atlas");
	await page.getByTestId("rename-agent-confirm").click();
	await expect(page.getByTestId("rename-agent-dialog")).toHaveCount(0);
	await expect(
		page.getByTestId("settings-agent-row").getByTestId("settings-agent-name"),
	).toHaveText(["atlas", "helper"]);

	// Only the name changed; its projects and tasks moved with it.
	expect(state.saves).toHaveLength(1);
	const renamed = state.saves[0].agents[0];
	expect(renamed.name).toBe("atlas");
	expect(renamed.projects).toEqual([{ target: "lsegal/aviary" }]);
	expect((renamed.tasks as { name: string }[]).map((t) => t.name)).toEqual([
		"daily",
	]);
});

test("Delete confirms with the agent's dependents before deleting", async ({
	page,
}) => {
	const state = await mockConfig(page, twoAgents);
	await page.goto("/system/settings");

	const row = page.locator('[data-testid=settings-agent-row][data-agent="assistant"]');
	await row.getByTestId("settings-agent-menu").click();
	await page.getByTestId("settings-agent-delete").click();
	const dialog = page.getByTestId("delete-agent-dialog");
	await expect(dialog).toContainText("Delete assistant?");
	await expect(page.getByTestId("delete-agent-dependents")).toContainText(
		"1 owned project, 1 scheduled task, 1 session",
	);

	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toHaveCount(0);
	expect(state.saves).toHaveLength(0);

	await row.getByTestId("settings-agent-menu").click();
	await page.getByTestId("settings-agent-delete").click();
	await page.getByTestId("delete-agent-confirm").click();
	await expect(dialog).toHaveCount(0);
	await expect(
		page.getByTestId("settings-agent-row").getByTestId("settings-agent-name"),
	).toHaveText(["helper"]);
	expect(state.saves).toHaveLength(1);
	expect(state.saves[0].agents.map((a) => a.name)).toEqual(["helper"]);

	// The last agent can't be deleted, and the menu says why.
	await page.getByTestId("settings-agent-menu").click();
	const del = page.getByTestId("settings-agent-delete");
	await expect(del).toHaveAttribute("data-disabled", "");
	await expect(del.locator("..")).toHaveAttribute(
		"title",
		/last agent can't be deleted/,
	);
});

test("Update automatically is on for a config that doesn't set it", async ({
	page,
}) => {
	await mockConfig(page, { agents: [{ name: "assistant", model: "" }] });
	await page.goto("/system/settings");
	await expect(page.getByTestId("auto-update")).toBeChecked();
});

test("Update automatically stays off when explicitly disabled", async ({
	page,
}) => {
	const state = await mockConfig(page, {
		server: { auto_update: false },
		agents: [{ name: "assistant", model: "" }],
	});
	await page.goto("/system/settings");
	const box = page.getByTestId("auto-update");
	await expect(box).not.toBeChecked();

	// Turning it on saves the default, which leaves the key unset.
	await box.check();
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => state.saves.length).toBe(1);
	expect(state.saves[0].server?.auto_update).toBeUndefined();
});
