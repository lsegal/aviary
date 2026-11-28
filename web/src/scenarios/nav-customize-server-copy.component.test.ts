import { expect, test } from "vitest";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { route } from "../component-test/server";
import { mockAgents, sidebarPages } from "./helpers/navCustomize";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/OverviewView.vue"));

// Customizing the navigation (#1003): a first load applies the server's
// saved order. In its own file because the preferences module reads the
// server only on first use, which module state keeps for the whole file.

test("a page missing from a saved order takes its default place", async () => {
	mockAgents(["assistant"]);
	// Saved by an older version: no Files page, plus one since removed.
	const saved = {
		order: {
			agentPages: ["settings", "tasks", "gone", "chat", "projects", "channels"],
		},
		hidden: {},
	};
	route("/api/preferences/navigation", (r) =>
		r.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(saved),
		}),
	);
	await renderApp("/system/overview");
	await expect
		.poll(sidebarPages)
		.toEqual(["settings", "tasks", "files", "chat", "projects", "channels"]);
});
