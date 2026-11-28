// The collapsed rail's scenario lives in its own file: collapsing the
// sidebar is kept in useSidebarLayout's module state, which would leave the
// sidebar collapsed for every later test in the same file (#1636).
import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import { mockChannels, TEAM } from "./helpers/channelsServer";

test("the Channels section works in the collapsed rail and the mobile nav", async () => {
	mockChannels([TEAM]);
	await renderApp("/agents/assistant/chat");
	await page.getByTestId("sidebar-collapse").click();
	const rail = page.getByTestId("sidebar-rail-channels");
	await expect.element(rail).toBeVisible();
	await rail.click();
	await expect.poll(currentURL).toMatch(/\/agents\/assistant\/channels$/);

	await page.viewport(390, 800);
	await goto("/agents/assistant/channels");
	await expect
		.element(page.getByTestId("mobile-row-action"))
		.toHaveAccessibleName("Add channel");
	await expect
		.element(
			page.getByRole("link", { name: "Team Slack", exact: true }).first(),
		)
		.toBeVisible();
});
