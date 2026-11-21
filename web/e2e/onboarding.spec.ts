import { expect, type Page, test } from "@playwright/test";
import { type ConfigSaveArgs, mockMCP, setAuthToken } from "./helpers/mockMCP";

/** A fresh install with a CLI already signed in: a provider, but no agent. */
async function mockFreshInstall(page: Page) {
	const agents: object[] = [];
	let saved: { agents?: { name: string }[] } | undefined;
	await mockMCP(page, {
		agent_list: () => agents,
		job_list: [],
		config_validate: [],
		session_list: [],
		auth_list: ["anthropic:default"],
		config_get: () => saved ?? { agents: [], models: { providers: {} } },
		config_save: (args?: ConfigSaveArgs) => {
			saved = JSON.parse(args?.config ?? "{}");
			for (const a of saved?.agents ?? [])
				agents.push({ id: a.name, name: a.name, state: "idle" });
			return {};
		},
	});
	return { saved: () => saved };
}

test("first login with only a detected provider opens the setup wizard (#860)", async ({
	page,
}) => {
	await mockFreshInstall(page);
	await page.goto("/login");
	await page.getByLabel("Token").fill("test-token");
	await page.keyboard.press("Enter");

	await expect(page).toHaveURL("/onboarding");
	await expect(page.getByText("Welcome to Aviary")).toBeVisible();
	await expect(page.getByTestId("sidebar-agents")).toHaveCount(0);
});

test("an agent-less chat link presents the wizard, and chat opens once the agent exists (#860)", async ({
	page,
}) => {
	await setAuthToken(page, "test-token", { onboarding: true });
	const { saved } = await mockFreshInstall(page);
	await page.goto("/agents/default/chat");

	await expect(page).toHaveURL("/onboarding");
	await page.getByRole("button", { name: /Anthropic/ }).click();
	await expect(page.getByPlaceholder("default")).toHaveValue("default");
	await page.getByRole("button", { name: "Create agent" }).click();
	await expect(page.getByText("is ready to go")).toBeVisible();
	expect(saved()?.agents?.map((a) => a.name)).toEqual(["default"]);

	await page.getByRole("link", { name: "Start chatting" }).click();
	await expect(page).toHaveURL("/agents/default/chat");
});

test("skipping the wizard lands in the app and stays skipped (#860)", async ({
	page,
}) => {
	await setAuthToken(page, "test-token", { onboarding: true });
	await mockFreshInstall(page);
	await page.goto("/system/overview");

	await expect(page).toHaveURL("/onboarding");
	await page.getByRole("button", { name: "Skip for now" }).click();
	await expect(page).toHaveURL("/system/overview");
	await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();

	await page.goto("/system/usage");
	await expect(page).toHaveURL("/system/usage");
});

test("with an agent configured, pages open without the wizard (#860)", async ({
	page,
}) => {
	await setAuthToken(page, "test-token", { onboarding: true });
	await mockMCP(page, {
		agent_list: [{ id: "a1", name: "bot", state: "idle" }],
		job_list: [],
		config_validate: [],
	});
	await page.goto("/system/overview");

	await expect(page).toHaveURL("/system/overview");
	await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
});
