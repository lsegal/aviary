import type { Page } from "@playwright/test";

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
function agentNav(page: Page) {
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
export async function openAgentPage(page: Page, tail: AgentPage, agent = "") {
	let name = agent;
	if (!name) {
		if (page.url() === "about:blank") await page.goto("/system/overview");
		name =
			(await page
				.getByTestId("sidebar-nav")
				.locator(
					"[data-testid=sidebar-agent-list] a[data-agent], [data-testid=sidebar-sole-agent]",
				)
				.first()
				.getAttribute("data-agent")) ?? "";
	}
	await page.goto(`/agents/${encodeURIComponent(name)}/${tail}`);
}

/** Opens a channel's settings form, by default the assistant agent's. */
export async function openChannelSettings(
	page: Page,
	channel: string,
	agent = "assistant",
) {
	await page.goto(
		`/agents/${encodeURIComponent(agent)}/channels/${encodeURIComponent(channel)}/settings`,
	);
}

/** The active agent's page link in the sidebar, such as Tasks or Files. */
export function agentPageLink(page: Page, label: string) {
	return agentNav(page).getByRole("link", { name: label, exact: true });
}
