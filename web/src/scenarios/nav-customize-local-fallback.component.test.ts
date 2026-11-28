import { expect, test } from "vitest";
import { reload, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { requestLog, route } from "../component-test/server";
import {
	DEFAULT_PAGES,
	editRow,
	mockAgents,
	openEditor,
	sidebarPages,
} from "./helpers/navCustomize";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/OverviewView.vue"),
	() => import("../views/SettingsView.vue"),
);

// Customizing the navigation (#1003): when the server can't keep the
// preferences, this browser's copy does. In its own file because the
// preferences module reads the server only on first use, which module state
// keeps for the whole file.

test("without the server's preferences, localStorage keeps them", async () => {
	mockAgents(["assistant"]);
	route("/api/preferences/navigation", (r) =>
		r.fulfill({ status: 500, body: "unavailable" }),
	);
	await renderApp("/system/overview");
	await openEditor();
	await editRow("agentPages", "channels")
		.getByTestId("nav-edit-visibility")
		.click();
	await expect
		.element(editRow("agentPages", "channels"))
		.toHaveAttribute("data-hidden", "true");
	// The server was asked, and failed.
	expect(
		requestLog().some(
			(r) =>
				r.url.endsWith("/api/preferences/navigation") && r.method === "GET",
		),
	).toBe(true);
	// reload() keeps module state, so check the copy a real reload reads.
	expect(
		JSON.parse(localStorage.getItem("aviary.nav.prefs") ?? "{}").hidden
			?.agentPages,
	).toEqual(["channels"]);

	await reload();
	await expect
		.poll(sidebarPages)
		.toEqual(DEFAULT_PAGES.filter((k) => k !== "channels"));
});
