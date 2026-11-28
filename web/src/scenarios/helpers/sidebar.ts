// Mirrors web/e2e/helpers/sidebar.ts for component tests (#1636).
import { expect } from "vitest";
import { page } from "vitest/browser";
import { goto, renderApp } from "../../component-test/app";

/** An agent's pages in the sidebar, as their URL tails. */
export type AgentPage =
	| "chat"
	| "settings"
	| "settings/permissions"
	| "channels"
	| "files"
	| "tasks"
	| "projects";

/**
 * The sidebar's agent pages: the Agents list, or with a single agent its
 * top-level pages (#538).
 */
function agentNav() {
	return page
		.getByTestId("sidebar-nav")
		.locator(
			"[data-testid=sidebar-agent-list], [data-testid=sidebar-sole-agent]",
		);
}

/**
 * Opens one of an agent's pages, by default the first agent's in the
 * sidebar's Agents list, or the sole agent's.
 */
export async function openAgentPage(tail: AgentPage, agent = "") {
	let name = agent;
	if (!name) {
		// No app mounted yet, as a spec's page still on about:blank.
		if (!document.getElementById("app")) await renderApp("/system/overview");
		const first = page
			.getByTestId("sidebar-nav")
			.locator(
				"[data-testid=sidebar-agent-list] a[data-agent], [data-testid=sidebar-sole-agent]",
			)
			.first();
		await expect.element(first).toBeInTheDocument();
		name = first.element().getAttribute("data-agent") ?? "";
	}
	await goto(`/agents/${encodeURIComponent(name)}/${tail}`);
}

/** Opens a channel's settings form, by default the assistant agent's. */
export async function openChannelSettings(
	channel: string,
	agent = "assistant",
) {
	await goto(
		`/agents/${encodeURIComponent(agent)}/channels/${encodeURIComponent(channel)}/settings`,
	);
}

/** The active agent's page link in the sidebar, such as Tasks or Files. */
export function agentPageLink(label: string) {
	return agentNav().getByRole("link", { name: label, exact: true });
}

/** The System section's page list, expanding the section when collapsed. */
export async function systemList() {
	const toggle = page.getByTestId("sidebar-system-toggle");
	await expect.element(toggle).toBeInTheDocument();
	if (toggle.element().getAttribute("aria-expanded") === "false")
		await toggle.click();
	return page.getByTestId("sidebar-system-list");
}

/** Opens one item of the sidebar footer's gear menu (#822). */
export async function openGearMenuItem(
	item: "Settings" | "Restart" | "Logout",
) {
	await page
		.getByTestId("sidebar-footer")
		.getByTestId("gear-menu-button")
		.click();
	await page
		.getByTestId("gear-menu")
		.getByRole("menuitem", { name: item, exact: true })
		.click();
}

/**
 * Opens a task's settings through its sidebar row's "…" → Settings, by
 * default the first task's. The row itself opens the task's session.
 */
export async function openTaskSettings(task = "") {
	const rows = page.getByTestId("sidebar-tasks").getByTestId("task-row");
	const row = task
		? rows.and(page.locator(`[data-task="${task}"]`))
		: rows.first();
	await row.hover();
	await row.getByTestId("task-menu-button").click();
	await page.getByRole("menuitem", { name: "Settings" }).click();
}
