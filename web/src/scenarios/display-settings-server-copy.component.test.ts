import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP, route } from "../component-test/server";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/SettingsView.vue"));

// Settings → Display (#1093): a device with no local copy reads the
// server's. Its own file because the display preferences load the server's
// copy only once per page (module state), which display-settings has
// already spent.

test("a device without a local copy applies the server's display preferences", async () => {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: [],
		auth_list: [],
		config_get: {
			agents: [{ name: "assistant", model: "x/y", fallbacks: [] }],
		},
	});
	route("/api/preferences/display", (r) =>
		r.fulfill({ json: { theme: "dark", textSize: "large" } }),
	);
	expect(localStorage.getItem("aviary.display.prefs")).toBeNull();
	await renderApp("/system/display");

	const htmlAttr = (name: string) =>
		document.documentElement.getAttribute(name);
	await expect.poll(() => htmlAttr("data-text-size")).toBe("large");
	await expect.poll(() => htmlAttr("data-theme")).toBe("dark");
	await expect
		.poll(() =>
			Number.parseFloat(getComputedStyle(document.documentElement).fontSize),
		)
		.toBe(18);
	await expect
		.element(
			page.getByTestId("display-text-size").locator('[data-value="large"]'),
		)
		.toHaveAttribute("aria-checked", "true");
	// Cached for index.html to apply before the next load's first paint.
	expect(
		JSON.parse(localStorage.getItem("aviary.display.prefs") ?? "{}"),
	).toEqual({
		theme: "dark",
		textSize: "large",
	});
});
