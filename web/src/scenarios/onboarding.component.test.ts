import { expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
// The pages this file renders (see component-test/routes.ts).
import "../views/ChatView.vue";
import "../views/LoginView.vue";
import "../views/OnboardingView.vue";
import "../views/OverviewView.vue";
import "../views/SettingsView.vue";

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

test("first login with only a detected provider opens the setup wizard (#860)", async () => {
	mockFreshInstall();
	await renderApp("/login", { token: null });
	await page.getByLabelText("Token").fill("test-token");
	await commands.keyPress("Enter");

	await expect.poll(currentURL).toBe("/onboarding");
	await expect.element(page.getByText("Welcome to Aviary")).toBeVisible();
	expect(page.getByTestId("sidebar-agents").elements()).toHaveLength(0);
});

test("an agent-less chat link presents the wizard, and chat opens once the agent exists (#860)", async () => {
	const { saved } = mockFreshInstall();
	await renderApp("/agents/default/chat", { onboarding: true });

	await expect.poll(currentURL).toBe("/onboarding");
	await page.getByRole("button", { name: /Anthropic/ }).click();
	await expect.element(page.getByPlaceholder("default")).toHaveValue("default");
	await page.getByRole("button", { name: "Create agent" }).click();
	await expect.element(page.getByText("is ready to go")).toBeVisible();
	expect(saved()?.agents?.map((a) => a.name)).toEqual(["default"]);

	await page.getByRole("link", { name: "Start chatting" }).click();
	await expect.poll(currentURL).toBe("/agents/default/chat");
});

test("with an agent configured, pages open without the wizard (#860)", async () => {
	mockMCP({
		agent_list: [{ id: "a1", name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await renderApp("/system/overview", { onboarding: true });

	await expect.poll(currentURL).toBe("/system/overview");
	await expect
		.element(page.getByRole("heading", { name: "Overview" }))
		.toBeVisible();
});
