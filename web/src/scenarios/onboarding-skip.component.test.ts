// Its own file: once a test in a file has seen an agent, lib/onboarding
// stops asking for that page, as it would for the rest of a real visit.
import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/OnboardingView.vue"),
	() => import("../views/OverviewView.vue"),
	() => import("../views/UsageView.vue"),
);

interface SavedConfig {
	agents?: { name: string }[];
}

/** A fresh install with a CLI already signed in: a provider, but no agent. */
function mockFreshInstall() {
	const agents: object[] = [];
	let saved: SavedConfig | undefined;
	mockMCP({
		agent_list: () => agents,
		job_list: [],
		config_validate: [],
		session_list: [],
		auth_list: ["anthropic:default"],
		config_get: () => saved ?? { agents: [], models: { providers: {} } },
		config_save: (args?: { config?: string }) => {
			saved = JSON.parse(args?.config ?? "{}");
			for (const a of saved?.agents ?? [])
				agents.push({ id: a.name, name: a.name, state: "idle" });
			return {};
		},
	});
	return { saved: () => saved };
}

test("skipping the wizard lands in the app and stays skipped (#860)", async () => {
	mockFreshInstall();
	await renderApp("/system/overview", { onboarding: true });

	await expect.poll(currentURL).toBe("/onboarding");
	await page.getByRole("button", { name: "Skip for now" }).click();
	await expect.poll(currentURL).toBe("/system/overview");
	await expect
		.element(page.getByRole("heading", { name: "Overview" }))
		.toBeVisible();

	await goto("/system/usage");
	await expect.poll(currentURL).toBe("/system/usage");
});
