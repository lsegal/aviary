import { beforeEach, expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import { nextTick } from "vue";
import { currentURL, goto, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import {
	SIDEBAR_DEFAULT_WIDTH,
	useSidebarLayout,
} from "../composables/useSidebarLayout";
// The pages this file renders (see component-test/routes.ts).
import "../views/OverviewView.vue";
import "../views/SettingsView.vue";

interface MockConfig {
	server?: Record<string, unknown>;
	agents: Record<string, unknown>[];
}

// config_get answers from the last saved config, so saves show on reload.
function mockConfig(initial: MockConfig) {
	const state = {
		config: initial,
		saves: [] as MockConfig[],
		deleteData: [] as unknown[],
	};
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: (args?: Record<string, unknown>) =>
			args?.agent === "assistant"
				? [{ id: "main", agent_id: "assistant", name: "main" }]
				: [],
		config_get: () => state.config,
		config_save: (args?: Record<string, unknown>) => {
			state.config = JSON.parse(String(args?.config ?? "{}"));
			state.saves.push(state.config);
			if (args?.delete_data) state.deleteData.push(args.delete_data);
			return "saved";
		},
	});
	return state;
}

const twoAgents = (): MockConfig => ({
	agents: [
		{
			name: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			tasks: [{ name: "daily", schedule: "0 9 * * *", prompt: "hi" }],
			projects: [{ target: "lsegal/aviary" }],
		},
		{ name: "helper", model: "openai/gpt-5" },
	],
});

/** The headings of System → Settings' sections, top to bottom. */
function sectionHeadings(): string[] {
	return page
		.locator("section h3")
		.elements()
		.filter((el) => (el as HTMLElement).offsetParent)
		.map((el) => (el.textContent ?? "").trim());
}

function agentNames(): string[] {
	return page
		.getByTestId("settings-agent-row")
		.getByTestId("settings-agent-name")
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
}

beforeEach(async () => {
	// The sidebar's layout lives in module state, which outlasts a test.
	const layout = useSidebarLayout();
	layout.collapsed.value = false;
	layout.width.value = SIDEBAR_DEFAULT_WIDTH;
	await nextTick();
});

test("the sidebar header has no + and the title is uncovered", async () => {
	mockConfig({ agents: [{ name: "assistant", model: "" }] });
	await renderApp("/system/overview");

	const title = page.getByTestId("sidebar-title");
	await expect.element(title).toHaveText("Aviary");
	expect(page.getByTestId("sidebar-add-agent").elements()).toHaveLength(0);
	// Nothing is drawn over the title's centre.
	const el = title.element();
	const r = el.getBoundingClientRect();
	const hit = document.elementFromPoint(
		r.left + r.width / 2,
		r.top + r.height / 2,
	);
	expect(hit !== el && !el.contains(hit)).toBe(false);

	// Collapsed, the rail's hover overlay shows the same header.
	await page.getByTestId("sidebar-collapse").click();
	await expect.element(page.getByTestId("sidebar-rail")).toBeVisible();
	// Rest below the rail's items, where the pointer opens the overlay.
	const rail = page
		.getByTestId("sidebar-rail")
		.element()
		.getBoundingClientRect();
	await commands.mouseMove(rail.x + rail.width / 2, rail.y + rail.height - 220);
	await expect
		.element(page.getByTestId("sidebar"))
		.toHaveAttribute("data-peek", "true");
	await expect.element(page.getByTestId("sidebar-title")).toHaveText("Aviary");
	expect(page.getByTestId("sidebar-add-agent").elements()).toHaveLength(0);
	// Move off the rail so the overlay doesn't outlive the test.
	await commands.mouseMove(800, 400);
});

test("Agents sits directly below Models and Server is last", async () => {
	mockConfig(twoAgents());
	await renderApp("/system/settings");

	await expect.element(page.getByTestId("settings-agents")).toBeVisible();
	const headings = sectionHeadings();
	expect(headings.indexOf("Agents")).toBe(headings.indexOf("Models") + 1);
	expect(headings.at(-1)).toBe("Server");

	const rows = page.getByTestId("settings-agent-row");
	await expect.poll(agentNames).toEqual(["assistant", "helper"]);
	await expect
		.element(rows.first())
		.toMatchTextContent("anthropic/claude-sonnet-4-5");
	await expect
		.element(rows.first().getByTestId("settings-agent-name"))
		.toHaveAttribute("href", "/agents/assistant/settings");
});

test("+ Add agent opens the create-agent flow", async () => {
	mockConfig(twoAgents());
	await renderApp("/system/settings");
	await page.getByTestId("settings-add-agent").click();
	await expect.poll(currentURL).toMatch(/\/agents\/_\d+\/settings$/);
	await page.locator('input[placeholder="assistant"]').fill("scout");
	await page.getByRole("button", { name: "Save Changes" }).click();

	await goto("/system/settings");
	await expect.poll(agentNames).toEqual(["assistant", "helper", "scout"]);
});

test("Rename rejects empty and duplicate names and saves the new name", async () => {
	const state = mockConfig(twoAgents());
	await renderApp("/system/settings");

	const row = page.locator(
		'[data-testid=settings-agent-row][data-agent="assistant"]',
	);
	await row.getByTestId("settings-agent-menu").click();
	await page.getByTestId("settings-agent-rename").click();
	const input = page.getByTestId("rename-agent-input");
	await expect.element(input).toHaveValue("assistant");

	await input.fill("  ");
	await page.getByTestId("rename-agent-confirm").click();
	await expect
		.element(page.getByTestId("rename-agent-error"))
		.toHaveText("Enter a name.");

	await input.fill("Helper");
	await page.getByTestId("rename-agent-confirm").click();
	await expect
		.element(page.getByTestId("rename-agent-error"))
		.toMatchTextContent("already exists");
	expect(state.saves).toHaveLength(0);

	await input.fill("atlas");
	await page.getByTestId("rename-agent-confirm").click();
	await expect
		.element(page.getByTestId("rename-agent-dialog"))
		.not.toBeInTheDocument();
	await expect.poll(agentNames).toEqual(["atlas", "helper"]);

	// Only the name changed; its projects and tasks moved with it.
	expect(state.saves).toHaveLength(1);
	const renamed = state.saves[0].agents[0];
	expect(renamed.name).toBe("atlas");
	expect(renamed.projects).toEqual([{ target: "lsegal/aviary" }]);
	expect((renamed.tasks as { name: string }[]).map((t) => t.name)).toEqual([
		"daily",
	]);
});

test("Delete confirms with the agent's dependents before deleting", async () => {
	const state = mockConfig(twoAgents());
	await renderApp("/system/settings");

	const row = page.locator(
		'[data-testid=settings-agent-row][data-agent="assistant"]',
	);
	await row.getByTestId("settings-agent-menu").click();
	await page.getByTestId("settings-agent-delete").click();
	const dialog = page.getByTestId("delete-agent-dialog");
	await expect.element(dialog).toMatchTextContent("Delete assistant?");
	await expect
		.element(page.getByTestId("delete-agent-dependents"))
		.toMatchTextContent("1 owned project, 1 scheduled task, 1 session");

	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect.element(dialog).not.toBeInTheDocument();
	expect(state.saves).toHaveLength(0);

	await row.getByTestId("settings-agent-menu").click();
	await page.getByTestId("settings-agent-delete").click();
	// Deleting the agent's data is offered, checked by default.
	await expect
		.element(page.getByTestId("delete-agent-data-checkbox"))
		.toBeChecked();
	await page.getByTestId("delete-agent-dialog-confirm").click();
	await expect.element(dialog).not.toBeInTheDocument();
	await expect.poll(agentNames).toEqual(["helper"]);
	expect(state.saves).toHaveLength(1);
	expect(state.saves[0].agents.map((a) => a.name)).toEqual(["helper"]);
	expect(state.deleteData).toEqual([[{ kind: "agent", name: "assistant" }]]);

	// The last agent can't be deleted, and the menu says why.
	await page.getByTestId("settings-agent-menu").click();
	const del = page.getByTestId("settings-agent-delete");
	await expect.element(del).toHaveAttribute("data-disabled", "");
	await expect
		.poll(() => del.element().parentElement?.getAttribute("title") ?? "")
		.toMatch(/last agent can't be deleted/);
	await commands.keyPress("Escape");
});

test("Update automatically is on for a config that doesn't set it", async () => {
	mockConfig({ agents: [{ name: "assistant", model: "" }] });
	await renderApp("/system/settings");
	await expect.element(page.getByTestId("auto-update")).toBeChecked();
});

test("Update automatically stays off when explicitly disabled", async () => {
	const state = mockConfig({
		server: { auto_update: false },
		agents: [{ name: "assistant", model: "" }],
	});
	await renderApp("/system/settings");
	const box = page.getByTestId("auto-update");
	await expect.element(box).not.toBeChecked();

	// Turning it on saves the default, which leaves the key unset.
	await box.click();
	await expect.element(box).toBeChecked();
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => state.saves.length).toBe(1);
	expect(state.saves[0].server?.auto_update).toBeUndefined();
});
