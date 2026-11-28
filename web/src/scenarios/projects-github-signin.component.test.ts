// Ported from web/e2e/projects.spec.ts (#1636): the GitHub sign-in banner.
import { describe, expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { openBoardInfo } from "./helpers/boardHeader";
import { browserStatus, SIGNED_OUT, STATUS, setup } from "./projects-fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/ProjectsView.vue";
import "../views/SettingsView.vue";

describe("GitHub sign-in banner", () => {
	test("a signed-out read shows the banner, never a window", async () => {
		const calls = setup(() => browserStatus(SIGNED_OUT));
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		const banner = page.getByTestId("github-signin-banner");
		await expect
			.element(banner)
			.toMatchTextContent("Signed out of GitHub in Aviary's browser");
		await expect.element(banner).toMatchTextContent("GitHub answered 404");
		await expect.element(banner).toMatchTextContent('This is not "no issues"');
		await expect
			.element(page.getByTestId("delivery-status"))
			.toHaveText("offline");
		const info = await openBoardInfo();
		await expect
			.element(info.getByTestId("board-delivery"))
			.toHaveText("browser");
		await expect
			.element(info.getByTestId("board-interval"))
			.toHaveText("every 20s");
		// Without a filter every open issue is picked up, with no label.
		await expect.element(info).toMatchTextContent("All open issues");
		await expect
			.poll(() => page.getByTestId("issue-filter").elements().length)
			.toBe(0);
		await commands.keyPress("Escape");
		// The page only reads status until the user acts.
		expect(calls.project_github_signin).toBeUndefined();

		await banner.getByRole("button", { name: "Sign in to GitHub" }).click();
		await expect.poll(() => calls.project_github_signin?.length).toBe(1);
		await banner.getByRole("button", { name: "Switch to gh polling" }).click();
		await expect.poll(() => calls.project_github_use_polling?.length).toBe(1);
		await expect
			.poll(() => banner.getByRole("textbox").elements().length)
			.toBe(0);
	});

	test("an open sign-in window replaces the buttons", async () => {
		setup(() => browserStatus({ ...SIGNED_OUT, signing_in: true }));
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		const banner = page.getByTestId("github-signin-banner");
		await expect
			.element(banner)
			.toMatchTextContent("Waiting for you to sign in to GitHub");
		await expect
			.poll(() => banner.getByRole("button").elements().length)
			.toBe(0);
	});

	test("without a display it offers gh polling or a profile import", async () => {
		const calls = setup(() =>
			browserStatus({
				...SIGNED_OUT,
				window_unavailable:
					"no display server (DISPLAY and WAYLAND_DISPLAY are both unset)",
				last_error: "",
			}),
		);
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		const banner = page.getByTestId("github-signin-banner");
		await expect
			.element(banner.getByTestId("github-signin-no-window"))
			.toMatchTextContent("no display server");
		await expect
			.poll(
				() =>
					banner.getByRole("button", { name: "Sign in to GitHub" }).elements()
						.length,
			)
			.toBe(0);
		const importButton = banner.getByRole("button", { name: "Import profile" });
		await expect.element(importButton).toBeDisabled();
		await banner
			.getByLabelText("Aviary browser profile to import")
			.fill(" /mnt/desktop/aviary/browser ");
		await importButton.click();
		await expect
			.poll(() => calls.project_github_import?.[0])
			.toEqual({ profile: "/mnt/desktop/aviary/browser" });
		await expect
			.element(banner.getByRole("button", { name: "Switch to gh polling" }))
			.toBeVisible();
	});

	test("a signed-in session and gh polling show no banner", async () => {
		let status: object = browserStatus({
			signed_out: false,
			signing_in: false,
		});
		setup(() => status);
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		await expect.element(page.getByTestId("job-card").first()).toBeVisible();
		await expect
			.poll(() => page.getByTestId("github-signin-banner").elements().length)
			.toBe(0);
		await expect
			.element(page.getByTestId("delivery-status"))
			.toHaveText("live");
		// Signing out mid-run raises the banner on the next poll, and signing in
		// again clears it.
		status = browserStatus(SIGNED_OUT);
		await expect
			.element(page.getByTestId("github-signin-banner"))
			.toBeVisible();
		status = browserStatus({ signed_out: false, signing_in: false });
		await expect
			.poll(() => page.getByTestId("github-signin-banner").elements().length)
			.toBe(0);
		status = STATUS;
		await expect
			.element((await openBoardInfo()).getByTestId("board-delivery"))
			.toHaveText("poll");
		await expect
			.poll(() => page.getByTestId("github-signin-banner").elements().length)
			.toBe(0);
	});
});
