import { describe, expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { mockTasks, openRunDetails, panelWidth } from "./tasks-fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/SettingsView.vue";

// The run details drawer reads its stored width once, when its module loads
// (#878), so a test that seeds that width needs a page of its own: in
// tasks.component.test.ts another test would have loaded it already.

describe("the run details drawer on a narrow viewport", () => {
	test("keeps its full-width layout with no resize handle", async () => {
		await page.viewport(375, 740);
		localStorage.setItem("aviary.detailsPanel.width", "700");
		mockTasks();
		await renderApp("/agents/assistant/tasks");
		await openRunDetails();
		await expect
			.poll(() => page.getByTestId("details-panel-resize").elements().length)
			.toBe(0);
		expect(panelWidth()).toBe(375);
	});
});
