import { beforeEach, describe, expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { route } from "../component-test/server";
import { mobileRowKeys, mockAgents } from "./helpers/navCustomize";
// The pages this file renders (see component-test/routes.ts).
import "../views/ChatView.vue";
import "../views/SettingsView.vue";

// Customizing the navigation (#1003): a top-row order saved before #1015 is
// dropped from the server's copy and this browser's. In its own file
// because the preferences module reads the server only on first use, which
// module state keeps for the whole file.

describe("the top row", () => {
	beforeEach(async () => {
		await page.viewport(390, 844);
	});

	test("drops a saved top-button order in favor of the Pages order", async () => {
		mockAgents(["assistant", "coder"]);
		// Saved before the top row followed the Pages order (#1015).
		let saved: unknown = {
			order: {
				agentPages: ["tasks", "chat"],
				top: ["system", "agent:coder", "agent:assistant"],
			},
			hidden: { agentPages: ["files"], top: ["agent:coder"] },
		};
		const puts: unknown[] = [];
		route("/api/preferences/navigation", async (r) => {
			if (r.request().method() === "PUT") {
				saved = r.request().postDataJSON();
				puts.push(saved);
			}
			await r.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify(saved),
			});
		});
		localStorage.setItem("aviary.nav.prefs", JSON.stringify(saved));
		const errors: string[] = [];
		const onError = (e: ErrorEvent) => errors.push(e.message);
		const onRejection = (e: PromiseRejectionEvent) =>
			errors.push(String(e.reason));
		window.addEventListener("error", onError);
		window.addEventListener("unhandledrejection", onRejection);

		try {
			await renderApp("/agents/assistant/chat");
			// Agents keep their configured order, and System stays last.
			await expect
				.poll(mobileRowKeys)
				.toEqual(["agent:assistant", "agent:coder", "system"]);
			// The agent's own pages still follow its saved Pages order.
			await expect
				.poll(() =>
					page
						.getByTestId("mobile-sub-nav")
						.locator("a")
						.elements()
						.map((el) => el.getAttribute("href")?.split("/").pop()),
				)
				// Settings and the hidden Files, missing from the saved order,
				// follow Scheduled Tasks as they do by default.
				.toEqual(["tasks", "settings", "chat", "projects", "channels"]);
			await expect.poll(() => puts.length).toBe(1);
			expect(puts[0]).toEqual({
				order: { agentPages: ["tasks", "chat"] },
				hidden: { agentPages: ["files"] },
			});
			expect(
				JSON.parse(localStorage.getItem("aviary.nav.prefs") ?? "{}"),
			).toEqual(puts[0]);
			expect(errors).toEqual([]);
		} finally {
			window.removeEventListener("error", onError);
			window.removeEventListener("unhandledrejection", onRejection);
		}
	});
});
