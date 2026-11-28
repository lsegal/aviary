import { describe, expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockTasks, openRunDetails, panelWidth } from "./tasks-fixtures";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/SettingsView.vue"));

// The run details drawer reads its stored width once, when its module loads
// (#878), so a test that seeds that width needs a page of its own: in
// tasks.component.test.ts another test would have loaded it already.

describe("the run details drawer on desktop", () => {
	test("clamps a stored width wider than the viewport allows", async () => {
		await page.viewport(1280, 800);
		localStorage.setItem("aviary.detailsPanel.width", "5000");
		mockTasks();
		await renderApp("/agents/assistant/tasks");
		await openRunDetails();
		await expect.poll(panelWidth).toBe(960);
		const overflow =
			document.documentElement.scrollWidth -
			document.documentElement.clientWidth;
		expect(overflow).toBeLessThanOrEqual(0);
	});
});
