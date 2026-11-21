import type { Page } from "@playwright/test";

/** An agent's pages in the sidebar, as their URL tails. */
export type AgentPage =
	| "chat"
	| "settings"
	| "settings/permissions"
	| "settings/channels"
	| "files"
	| "tasks"
	| "projects";

/**
 * Opens one of an agent's pages, by default the first agent's in the
 * sidebar's Agents list.
 */
export async function openAgentPage(page: Page, tail: AgentPage, agent = "") {
	let name = agent;
	if (!name) {
		if (page.url() === "about:blank") await page.goto("/system/overview");
		name =
			(await page
				.getByTestId("sidebar-agent-list")
				.locator("a[data-agent]")
				.first()
				.getAttribute("data-agent")) ?? "";
	}
	await page.goto(`/agents/${encodeURIComponent(name)}/${tail}`);
}

/** The active agent's page link in the sidebar, such as Tasks or Files. */
export function agentPageLink(page: Page, label: string) {
	return page
		.getByTestId("sidebar-agent-list")
		.getByRole("link", { name: label, exact: true });
}
