// Shared by the nav-customize*.component.test.ts scenarios (#1003, #1636),
// which are split across files because some need the navigation
// preferences module freshly loaded.

import { expect } from "vitest";
import { type Locator, page } from "vitest/browser";
import { currentURL } from "../../component-test/app";
import { mockMCP } from "../../component-test/server";

export const DEFAULT_PAGES = [
	"chat",
	"projects",
	"channels",
	"tasks",
	"files",
	"settings",
];

export function mockAgents(names: string[]): void {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: [],
		config_get: {
			agents: names.map((name) => ({
				name,
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
			})),
		},
	});
}

/** The sole agent's sidebar page rows, by key, in order. */
export async function sidebarPages(): Promise<string[]> {
	const rows = page
		.getByTestId("sidebar-sole-agent")
		.locator('[data-testid^="sidebar-row-"]');
	await expect.element(rows.first()).toBeVisible();
	return rows
		.elements()
		.map(
			(el) => el.getAttribute("data-testid")?.replace("sidebar-row-", "") ?? "",
		);
}

export async function systemPages(): Promise<string[]> {
	const links = page
		.getByTestId("sidebar-system-list")
		.locator('[data-testid^="sidebar-system-"]');
	await expect.element(links.first()).toBeVisible();
	return links
		.elements()
		.map(
			(el) =>
				el.getAttribute("data-testid")?.replace("sidebar-system-", "") ?? "",
		);
}

export async function openEditor(): Promise<void> {
	await page.getByTestId("sidebar").getByTestId("gear-menu-button").click();
	await page.getByTestId("gear-menu-edit-nav").click();
	await expect.poll(currentURL).toBe("/system/display#sidebar");
	await expect.element(page.getByTestId("nav-edit-agentPages")).toBeVisible();
}

export function editRow(list: string, key: string): Locator {
	return page.getByTestId(`nav-edit-${list}`).locator(`[data-key="${key}"]`);
}

/** A section's rows in the sidebar editor, by key, in order. */
export function rowKeys(list: string): string[] {
	return page
		.getByTestId(`nav-edit-${list}`)
		.getByTestId("nav-edit-row")
		.elements()
		.map((el) => el.getAttribute("data-key") ?? "");
}

/** The narrow-screen main row's keys, in order. */
export function mobileRowKeys(): (string | null)[] {
	return page
		.getByTestId("mobile-main-nav")
		.locator("a")
		.elements()
		.map((el) => el.getAttribute("data-key"));
}
