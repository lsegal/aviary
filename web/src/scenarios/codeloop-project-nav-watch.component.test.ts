import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import {
	backToBoard,
	expectSharedNavOnly,
	job,
	pill,
	setup,
} from "./codeloop-project-nav.fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/ProjectsView.vue";
import "../views/SettingsView.vue";

// The `aviary watch` session cases of codeloop-project-nav (#1044, #1066),
// in their own file: the signed-out health check they load is module state
// that would carry into other tests.

const WATCH = { ok: true, version: "dev", auth_disabled: true, minimal: true };

test("in an aviary watch session the shared breadcrumb leads back to the board", async () => {
	setup(() => [], WATCH);
	await renderApp("/agents/coder/projects/lsegal%2Fforgo", { token: null });
	await expectSharedNavOnly();
	await backToBoard();
});

test("an `aviary watch` session's Code Loop has the project pills", async () => {
	setup(() => [job("lsegal/aviary", 1)], WATCH);
	await renderApp("/agents/coder/projects", { token: null });
	await pill("lsegal/forgo").getByTestId("project-nav-scheduled").click();
	await expect
		.poll(currentURL)
		.toBe("/agents/coder/projects/lsegal%2Fforgo?state=scheduled");
	await expect.element(page.getByText("nightly-triage").first()).toBeVisible();
});
