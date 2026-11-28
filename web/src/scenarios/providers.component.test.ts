import { expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import modelCatalog from "../../../internal/models/catalog.json" with {
	type: "json",
};
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP, type ToolFixtures } from "../component-test/server";
import { systemList } from "./helpers/sidebar";
// The pages this file renders (see component-test/routes.ts).
import "../views/SettingsView.vue";
import "../views/SystemModelsView.vue";

// The CLI providers list's cached and default-model scenarios live in
// providers-cache and providers-default-models: the CLI list is module
// state, read from localStorage once, so each needs a fresh page.

const CLI_DEFINITIONS = { acme: { binary: "acme-cli" } };

const CLIS = {
	clis: [
		{
			name: "acme",
			installed: false,
			binary: "acme-cli",
			auth: "unknown",
			status: "missing",
		},
		{
			name: "codex",
			installed: true,
			binary: "codex",
			path: "/usr/local/bin/codex",
			version: "codex-cli 0.130.0",
			auth: "signed in",
			quota: "72% of weekly quota left",
			tracksQuota: true,
			models: ["codex/gpt-5.5", "codex/gpt-5.5-mini"],
			defaultModel: "gpt-5.5",
			skillsNote:
				"gh-fix stale for this aviary dev build; update them under System > Skills",
			status: "ok",
		},
		{
			name: "gemini",
			installed: true,
			binary: "gemini",
			path: "/opt/homebrew/bin/gemini",
			version: "0.40.1",
			auth: "signed out",
			status: "warn",
			statusNote: "signed out",
		},
		{
			name: "agy",
			installed: true,
			binary: "agy",
			path: "/usr/local/bin/agy",
			version: "1.0.0",
			auth: "unknown",
			status: "unknown",
			statusNote: "sign-in state unknown",
		},
		{
			name: "muse",
			installed: false,
			binary: "muse",
			auth: "unknown",
			modelNote: "any model the CLI accepts",
			status: "missing",
		},
	],
};

function setup(
	onSave?: (config: Record<string, unknown>) => void,
	extra: ToolFixtures = {},
) {
	const calls: string[] = [];
	mockMCP({
		config_get: {
			server: { tls: { cert: "", key: "" } },
			agents: [],
			models: {
				providers: { anthropic: { auth: "auth:anthropic:default" } },
				defaults: { model: "", fallbacks: [] },
				cli_definitions: CLI_DEFINITIONS,
			},
			browser: { binary: "", reuse_tabs: true },
			scheduler: { concurrency: "" },
			skills: {},
		},
		config_save: (args?: { config?: unknown }) => {
			onSave?.(JSON.parse(String(args?.config ?? "{}")));
			return {};
		},
		auth_list: ["anthropic:default"],
		skills_list: [],
		tool_list: [],
		provider_cli_list: () => {
			calls.push("provider_cli_list");
			return CLIS;
		},
		...extra,
	});
	return calls;
}

test("Settings has General, Providers & Authentication and Display tabs (#822, #1093)", async () => {
	setup();
	await renderApp("/system/settings");

	const tabs = page.getByTestId("settings-tabs").getByRole("tab");
	await expect
		.poll(() =>
			tabs
				.elements()
				.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim()),
		)
		.toEqual([
			"General",
			"Providers & Authentication",
			"Display",
			"Notifications",
		]);
	await expect.element(tabs.nth(0)).toHaveAttribute("aria-selected", "true");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Settings");

	await tabs.nth(1).click();
	await expect.poll(currentURL).toBe("/system/providers");
	await expect.element(tabs.nth(1)).toHaveAttribute("aria-selected", "true");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Settings");
	await expect.element(page.getByTestId("providers-list")).toBeVisible();
	// Providers & Auth is no longer a System page in the sidebar.
	// The System section's expanded state is module state: expand it only
	// when collapsed, as a fresh page's toggle click does.
	const systemPages = await systemList();
	await expect.element(systemPages).toBeVisible();
	await expect
		.poll(
			() =>
				systemPages.getByRole("link", { name: /Providers/ }).elements().length,
		)
		.toBe(0);
}, 90_000);

test("saving settings keeps the CLI definitions", async () => {
	let saved: Record<string, unknown> | null = null;
	setup((config) => {
		saved = config;
	});
	await renderApp("/system/settings");
	const expose = page.getByRole("checkbox", {
		name: /Expose service externally/i,
	});
	await expect.element(expose).toBeInTheDocument();
	if (!(expose.element() as HTMLInputElement).checked) await expose.click();
	await expect.element(expose).toBeChecked();
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.poll(() => saved).not.toBeNull();
	expect(saved).toMatchObject({
		models: { cli_definitions: CLI_DEFINITIONS },
	});
});

test("an Add provider row expands into tiles of provider kinds that start the add flow", async () => {
	const toolCalls: Array<Record<string, unknown>> = [];
	setup(undefined, {
		auth_set: (args?: Record<string, unknown>) => {
			toolCalls.push(args ?? {});
			return {};
		},
	});
	await renderApp("/system/providers");
	const card = page.getByTestId("providers-list");
	await expect.element(card).toBeVisible();
	await expect.poll(() => card.locator("select").elements().length).toBe(0);

	// The add row sits in the providers list itself, after the last provider.
	const row = card.getByTestId("provider-add-row");
	await expect.element(row).toHaveText(/Add provider/);
	// It is styled as a standard button: no dashed outline, sized to its label.
	await expect.element(row).not.toHaveCSS("border-top-style", "dashed");
	const rowBox = row.element().getBoundingClientRect();
	const cardBox = card.element().getBoundingClientRect();
	expect(rowBox.width).toBeLessThan(cardBox.width / 2);
	await row.click();
	const tiles = card
		.getByRole("group", { name: "Provider kinds" })
		.getByRole("button");
	// The same kinds the select offered: Anthropic's stored API key is not offered again.
	await expect
		.element(card.locator('[data-kind="anthropic:oauth"]'))
		.toBeVisible();
	await expect
		.element(card.locator('[data-kind="openai:apikey"]'))
		.toMatchTextContent("API Key");
	await expect
		.element(card.locator('[data-kind="vllm:endpoint"]'))
		.toMatchTextContent("Endpoint");
	await expect
		.poll(
			() => card.locator('[data-kind="anthropic:apikey"]').elements().length,
		)
		.toBe(0);

	// Arrow keys move between tiles; Escape collapses back to the focused row.
	await expect.element(tiles.first()).toHaveFocus();
	await commands.keyPress("ArrowRight");
	await expect.element(tiles.nth(1)).toHaveFocus();
	await commands.keyPress("End");
	await expect.element(tiles.last()).toHaveFocus();
	await commands.keyPress("Home");
	await expect.element(tiles.first()).toHaveFocus();
	await commands.keyPress("Escape");
	await expect.poll(() => tiles.elements().length).toBe(0);
	await expect.element(row).toHaveFocus();

	// An outside click collapses it too.
	await row.click();
	await expect.element(tiles.first()).toBeVisible();
	await page.getByText("Extra Secrets", { exact: true }).click();
	await expect.poll(() => tiles.elements().length).toBe(0);

	// Choosing a kind from the keyboard opens that kind's form in place.
	(row.element() as HTMLElement).focus();
	await commands.keyPress("Enter");
	await expect
		.element(card.locator('[data-kind="openai:apikey"]'))
		.toBeVisible();
	(
		card.locator('[data-kind="openai:apikey"]').element() as HTMLElement
	).focus();
	await commands.keyPress("Enter");
	const form = card.getByTestId("provider-add-form");
	await expect.element(form).toMatchTextContent("OpenAI (API Key)");
	await expect.element(row).not.toBeInTheDocument();
	await form.getByPlaceholder("API key…").fill("sk-test");
	await form.getByRole("button", { name: "Add", exact: true }).click();
	await expect
		.poll(() => toolCalls)
		.toEqual([{ name: "openai:default", value: "sk-test" }]);
	await expect.element(form).not.toBeInTheDocument();
	await expect.element(row).toBeVisible();

	// Cancel drops the chosen kind without adding anything.
	await row.click();
	await card.locator('[data-kind="vllm:endpoint"]').click();
	await expect
		.element(form.getByPlaceholder("Base URI (optional)"))
		.toBeVisible();
	await form.getByTestId("provider-add-cancel").click();
	await expect.element(form).not.toBeInTheDocument();
});

test("System → Models lists installed CLI providers' models alongside the catalog (#665)", async () => {
	setup();
	await renderApp("/system/models");

	const catalogProviders = new Set(
		modelCatalog.map((entry) => entry.id.split("/", 1)[0]),
	);
	// codex is the only installed CLI that reports models; muse and acme are
	// not installed, so the pickers do not offer them and neither does this page.
	await expect
		.element(page.getByTestId("models-count"))
		.toHaveText(String(modelCatalog.length + 2));
	await expect
		.element(page.getByTestId("providers-count"))
		.toHaveText(String(catalogProviders.size + 1));
	await expect
		.element(page.locator('[data-provider-filter="codex"]'))
		.toBeVisible();
	await expect
		.poll(() => page.locator('[data-provider-filter="muse"]').elements().length)
		.toBe(0);

	await page
		.getByPlaceholder("Search provider/model pairs")
		.fill("codex/gpt-5.5");
	const card = page.locator('[data-model="codex/gpt-5.5"]');
	await expect.element(card).toBeVisible();
	await expect.element(card).toMatchTextContent("via CLI");
	await expect
		.element(card)
		.toMatchTextContent("Reached through the installed codex CLI");
	// A CLI pair has no catalog data, so the card invents no token sizes.
	await expect.element(card).not.toMatchTextContent("Input");

	await page.getByPlaceholder("Search provider/model pairs").fill("");
	await page.locator('[data-provider-filter="codex"]').click();
	await expect.element(page.getByText("Showing 2 models")).toBeVisible();
});
